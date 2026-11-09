package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type TestCase struct {
	Name    string  `json:"name"`
	Status  string  `json:"status"` // pass | fail | skip
	Output  string  `json:"output"`
	Elapsed float64 `json:"elapsed"` // секунды
	Hidden  bool    `json:"hidden,omitempty"`
}

type TestResult struct {
	BuildFailed bool       `json:"buildFailed"`
	BuildOutput string     `json:"buildOutput,omitempty"`
	Passed      bool       `json:"passed"` // пакет целиком прошёл
	Tests       []TestCase `json:"tests"`
	Output      string     `json:"output,omitempty"` // вывод вне тестов: паника, таймаут
	TimedOut    bool       `json:"timedOut"`
	Truncated   bool       `json:"truncated"`
	DurationMs  int64      `json:"durationMs"`
}

// Test собирает тест-бинарник, запускает его в песочнице и разбирает
// результат в формате go test -json.
func (e *Executor) Test(ctx context.Context, files Files, flags Flags) (*TestResult, error) {
	uid, err := e.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer e.release(uid)
	ws, err := e.newWorkspace(ctx, uid, files)
	var genErr *GenerateError
	if errors.As(err, &genErr) {
		return &TestResult{BuildFailed: true, BuildOutput: genErr.Output, Tests: []TestCase{}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer ws.remove()

	args := []string{"test", "-c", "-o", "pkg.test"}
	if flags.Race {
		args = append(args, "-race")
	}
	if out, err := e.goTool(ctx, ws.dir, flags, append(args, ".")...); err != nil {
		if nerr := buildNoSpace(ws.dir); nerr != nil {
			return nil, nerr
		}
		return &TestResult{BuildFailed: true, BuildOutput: out, Tests: []TestCase{}}, nil
	}
	if _, err := os.Stat(filepath.Join(ws.dir, "pkg.test")); err != nil {
		// go test -c без _test.go-файлов ничего не создаёт.
		return &TestResult{BuildFailed: true, BuildOutput: "в пакете нет тестов", Tests: []TestCase{}}, nil
	}

	p := e.exec(ctx, ws, flags, "./pkg.test", "-test.v=test2json", "-test.timeout="+e.cfg.WallTime.String())
	res := &TestResult{Tests: []TestCase{}, TimedOut: p.timedOut, Truncated: p.truncated, DurationMs: p.duration.Milliseconds()}
	events, err := e.toJSON(ctx, p.stdout)
	if err != nil {
		return nil, err
	}
	parseEvents(res, events)
	if p.stderr != "" {
		res.Output += p.stderr
	}
	if p.timedOut || p.truncated {
		res.Passed = false
	}
	return res, nil
}

// toJSON прогоняет вывод -test.v=test2json через test2json, как это делает go test -json.
// Если бинарник упал без итоговой строки, parseEvents сам считает пакет непрошедшим.
func (e *Executor) toJSON(ctx context.Context, raw string) ([]byte, error) {
	// Начиная с Go 1.24 test2json собирается по требованию, поэтому через go tool.
	cmd := exec.CommandContext(ctx, "go", "tool", "test2json", "-t", "-p", "lesson")
	cmd.Stdin = strings.NewReader(raw)
	return cmd.Output()
}

type testEvent struct {
	Action  string
	Test    string
	Output  string
	Elapsed float64
}

func parseEvents(res *TestResult, events []byte) {
	index := map[string]int{}
	outputs := map[string]*strings.Builder{}
	sawPackageResult := false
	dec := json.NewDecoder(bytes.NewReader(events))
	for dec.More() {
		var ev testEvent
		if err := dec.Decode(&ev); err != nil {
			break
		}
		if ev.Test == "" {
			switch ev.Action {
			case "output":
				res.Output += ev.Output
			case "pass":
				res.Passed, sawPackageResult = true, true
			case "fail":
				res.Passed, sawPackageResult = false, true
			}
			continue
		}
		switch ev.Action {
		case "run":
			index[ev.Test] = len(res.Tests)
			res.Tests = append(res.Tests, TestCase{Name: ev.Test})
			outputs[ev.Test] = &strings.Builder{}
		case "output":
			if b := outputs[ev.Test]; b != nil && !isFrame(ev.Output) {
				b.WriteString(ev.Output)
			}
		case "pass", "fail", "skip":
			if i, ok := index[ev.Test]; ok {
				res.Tests[i].Status = ev.Action
				res.Tests[i].Elapsed = ev.Elapsed
				res.Tests[i].Output = outputs[ev.Test].String()
			}
		}
	}
	// Тест, начатый, но не завершённый, — значит процесс упал или был убит.
	for i := range res.Tests {
		if res.Tests[i].Status == "" {
			res.Tests[i].Status = "fail"
			res.Tests[i].Output = outputs[res.Tests[i].Name].String()
		}
	}
	if !sawPackageResult {
		res.Passed = false
	}
}

// isFrame отсекает служебные строки, которые для ученика — шум.
func isFrame(line string) bool {
	t := strings.TrimSpace(line)
	return strings.HasPrefix(t, "=== RUN") || strings.HasPrefix(t, "=== PAUSE") || strings.HasPrefix(t, "=== CONT") ||
		strings.HasPrefix(t, "--- PASS") || strings.HasPrefix(t, "--- FAIL") || strings.HasPrefix(t, "--- SKIP")
}

// Режимы GoTest — кнопки песочницы у примеров с тестами.
const (
	ModeTest  = "test"  // go test -v
	ModeBench = "bench" // только бенчмарки
	ModeFuzz  = "fuzz"  // fuzzing одной цели
	ModeCover = "cover" // go test -v -cover и отчёт по функциям
)

// TestOptions — что запускает GoTest.
type TestOptions struct {
	Mode string
	// Fuzz — имя функции FuzzXxx для ModeFuzz.
	Fuzz string
	// FuzzTime — сколько фаззить; 0 — DefaultFuzzTime.
	FuzzTime time.Duration
}

const (
	// DefaultFuzzTime оставляет запас до лимита времени запуска: процесс
	// фаззера ещё собирает базовое покрытие и минимизирует найденный вход.
	DefaultFuzzTime = 4 * time.Second
	// BenchTime короче привычной секунды, чтобы несколько бенчмарков
	// укладывались в лимит CPU.
	BenchTime     = "200ms"
	maxFuzzInput  = 4 << 10
	fuzzFileLimit = 128 << 20
)

var fuzzName = regexp.MustCompile(`^Fuzz[A-Za-z0-9_]*$`)

// GoTestResult — вывод go test как есть (а не разобранный по тестам, как у
// Test) и то, что режим оставил после себя.
type GoTestResult struct {
	RunResult
	// Coverage — отчёт go tool cover -func для ModeCover.
	Coverage string `json:"coverage,omitempty"`
	// FuzzInputs — входы, на которых фаззер нашёл ошибку.
	FuzzInputs []FuzzInput `json:"fuzzInputs,omitempty"`
}

// FuzzInput — файл корпуса testdata/fuzz/<цель>/<хеш>.
type FuzzInput struct {
	Name string `json:"name"`
	Data string `json:"data"`
}

// GoTest запускает go test для примера урока: файлы присылает ученик целиком,
// включая _test.go, а вывод возвращается в привычном виде go test -v.
func (e *Executor) GoTest(ctx context.Context, files Files, flags Flags, opts TestOptions) (*GoTestResult, error) {
	build := []string{"test", "-c", "-o", "pkg.test"}
	run := []string{"-test.timeout=" + e.cfg.WallTime.String()}
	switch opts.Mode {
	case ModeTest:
		run = append(run, "-test.v")
	case ModeCover:
		build = append(build, "-cover")
		run = append(run, "-test.v", "-test.coverprofile=cover.out")
	case ModeBench:
		// Детектор гонок искажает замеры в разы.
		flags.Race = false
		run = append(run, "-test.run=^$", "-test.bench=.", "-test.benchmem", "-test.benchtime="+BenchTime)
	case ModeFuzz:
		if !fuzzName.MatchString(opts.Fuzz) {
			return nil, &InputError{fmt.Errorf("недопустимое имя fuzz-теста %q", opts.Fuzz)}
		}
		flags.Race = false
		fuzzTime := opts.FuzzTime
		if fuzzTime <= 0 {
			fuzzTime = DefaultFuzzTime
		}
		// -fuzz при сборке включает инструментирование покрытия, без него
		// фаззер перебирает входы вслепую. Один воркер — чтобы уложиться в лимит процессов и CPU.
		build = append(build, "-fuzz=^"+opts.Fuzz+"$")
		run = append(run, "-test.run=^$", "-test.fuzz=^"+opts.Fuzz+"$", "-test.fuzztime="+fuzzTime.String(),
			"-test.parallel=1", "-test.fuzzcachedir=fuzzcache")
	default:
		return nil, &InputError{fmt.Errorf("неизвестный режим %q", opts.Mode)}
	}
	if flags.Race {
		build = append(build, "-race")
	}

	uid, err := e.acquire(ctx)
	if err != nil {
		return nil, err
	}
	defer e.release(uid)
	ws, err := e.newWorkspace(ctx, uid, files)
	var genErr *GenerateError
	if errors.As(err, &genErr) {
		return &GoTestResult{RunResult: RunResult{BuildFailed: true, BuildOutput: genErr.Output}}, nil
	}
	if err != nil {
		return nil, err
	}
	defer ws.remove()
	if opts.Mode == ModeFuzz {
		// Координатор фаззера делит память с воркером через файл в TMPDIR
		// и сразу растягивает его до 100 МБ (файл разреженный, диск почти не занят).
		ws.fileLimit = fuzzFileLimit
	}

	if out, err := e.goTool(ctx, ws.dir, flags, append(build, ".")...); err != nil {
		if nerr := buildNoSpace(ws.dir); nerr != nil {
			return nil, nerr
		}
		return &GoTestResult{RunResult: RunResult{BuildFailed: true, BuildOutput: out}}, nil
	}
	if _, err := os.Stat(filepath.Join(ws.dir, "pkg.test")); err != nil {
		return &GoTestResult{RunResult: RunResult{BuildFailed: true, BuildOutput: "в пакете нет тестов: добавьте файл *_test.go"}}, nil
	}
	p := e.exec(ctx, ws, flags, "./pkg.test", run...)
	res := &GoTestResult{RunResult: RunResult{
		Stdout:     p.stdout,
		Stderr:     p.stderr,
		ExitCode:   p.exitCode,
		TimedOut:   p.timedOut,
		Truncated:  p.truncated,
		DurationMs: p.duration.Milliseconds(),
	}}
	switch opts.Mode {
	case ModeCover:
		res.Coverage, err = e.coverFunc(ctx, ws, files, flags)
	case ModeFuzz:
		res.FuzzInputs, err = fuzzInputs(ws.dir, opts.Fuzz)
	}
	return res, err
}

// coverFunc строит отчёт по функциям из профиля покрытия. Программа могла
// подменить исходники или профиль симлинками, поэтому исходники пишутся
// заново, а профиль читается без перехода по ссылкам и кладётся рядом.
func (e *Executor) coverFunc(ctx context.Context, ws *workspace, files Files, flags Flags) (string, error) {
	profile, err := readCapped(filepath.Join(ws.dir, "cover.out"), maxCollect)
	if errors.Is(err, os.ErrNotExist) {
		return "", nil // тесты упали раньше, чем профиль записался
	}
	if err != nil {
		return "", err
	}
	for name, src := range files {
		path := filepath.Join(ws.dir, name)
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			return "", err
		}
	}
	tmp, err := os.CreateTemp(e.cfg.WorkRoot, "cover-*.out")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	_, err = tmp.Write(profile)
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", err
	}
	out, err := e.goTool(ctx, ws.dir, flags, "tool", "cover", "-func="+tmp.Name())
	if err != nil {
		return "", fmt.Errorf("go tool cover: %v: %s", err, out)
	}
	// Пути вида lesson/main.go:12: — имя модуля песочницы ученику ничего не говорит.
	return strings.ReplaceAll(out, "lesson/", ""), nil
}

// fuzzInputs читает входы, которые фаззер записал в testdata/fuzz/<цель>.
// os.Root не даёт выйти за рабочий каталог по симлинкам программы.
func fuzzInputs(dir, target string) ([]FuzzInput, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	sub := filepath.Join("testdata", "fuzz", target)
	d, err := root.Open(sub)
	if err != nil {
		return nil, nil // фаззер ничего не нашёл
	}
	entries, err := d.ReadDir(8)
	d.Close()
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, nil
	}
	var inputs []FuzzInput
	for _, ent := range entries {
		if !ent.Type().IsRegular() {
			continue
		}
		f, err := root.Open(filepath.Join(sub, ent.Name()))
		if err != nil {
			continue
		}
		data, err := io.ReadAll(io.LimitReader(f, maxFuzzInput))
		f.Close()
		if err != nil {
			continue
		}
		inputs = append(inputs, FuzzInput{Name: filepath.ToSlash(filepath.Join(sub, ent.Name())), Data: string(data)})
	}
	return inputs, nil
}
