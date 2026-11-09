package main

import (
	"bytes"
	"context"
	"errors"
	"go/format"
	"os"
	osexec "os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"gotutor/runner/livetrace"
	"gotutor/runner/sandbox"
)

// Checker прогоняет уроки через ту же песочницу, что и runner: сборка,
// лимиты и разбор go test -json в CI и в браузере совпадают.
type Checker struct {
	Exec   *sandbox.Executor
	Update bool // перезаписать example/output.txt реальным выводом
	// Sqlc — путь к sqlc; пусто — код примеров с sqlc.yaml не сверяется.
	Sqlc string
}

func (c *Checker) CheckLesson(ctx context.Context, l *Lesson, p *Problems) {
	c.checkFormat(l, p)
	c.checkExample(ctx, l, p)
	if l.HasTask {
		c.checkTask(ctx, l, p)
	}
	c.checkQuiz(ctx, l, p)
	if l.Meta.Govulncheck != nil {
		c.checkVulncheck(ctx, l, p)
	}
	if l.HasLive {
		c.checkLive(ctx, l, p)
	}
	if _, err := os.Stat(filepath.Join(l.Dir, "example", "sqlc.yaml")); err == nil {
		c.checkSqlc(ctx, l, p)
	}
	for _, dir := range []string{"example", "task"} {
		c.checkProto(ctx, l, dir, p)
	}
	if l.Meta.Buf != nil {
		c.checkBufLint(ctx, l, p)
	}
}

// checkProto требует, чтобы закоммиченный код по .proto совпадал с тем,
// что сгенерирует runner: ученик видит в уроке тот же код, что собирается.
func (c *Checker) checkProto(ctx context.Context, l *Lesson, dir string, p *Problems) {
	protos, _ := filepath.Glob(filepath.Join(l.Dir, dir, "*.proto"))
	if len(protos) == 0 {
		return
	}
	files := sandbox.Files{}
	entries, _ := os.ReadDir(filepath.Join(l.Dir, dir))
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".proto") && name != "buf.yaml" && !sandbox.IsGenerated(name) {
			continue
		}
		b, err := os.ReadFile(filepath.Join(l.Dir, dir, name))
		if err != nil {
			p.Errorf("%s: %v", dir, err)
			return
		}
		files[name] = string(b)
	}
	gen, err := c.Exec.GenerateFiles(ctx, files)
	var genErr *sandbox.GenerateError
	switch {
	case errors.As(err, &genErr):
		p.Errorf("%s: buf generate:\n%s", dir, indent(genErr.Output))
		return
	case err != nil:
		p.Errorf("%s: buf generate: %v", dir, err)
		return
	}
	for name, src := range gen {
		switch {
		case files[name] == src:
		case c.Update:
			if err := os.WriteFile(filepath.Join(l.Dir, dir, name), []byte(src), 0o644); err != nil {
				p.Errorf("%v", err)
			}
		default:
			p.Errorf("%s/%s: не совпадает с buf generate — перегенерируйте код: go run ./tools/lessonlint -update", dir, name)
		}
	}
	for name := range files {
		if sandbox.IsGenerated(name) && gen[name] == "" {
			p.Errorf("%s/%s: такого файла buf generate не создаёт — удалите его", dir, name)
		}
	}
}

// checkBufLint прогоняет buf lint по примеру, как кнопка в песочнице, и
// требует нарушений из buf.expectLint (пусто — контракт чистый).
func (c *Checker) checkBufLint(ctx context.Context, l *Lesson, p *Problems) {
	files, err := exampleFiles(l)
	if err != nil {
		return
	}
	res, err := c.Exec.BufCheck(ctx, files, nil)
	switch {
	case errors.Is(err, sandbox.ErrNoBuf):
		p.Errorf("buf lint: buf не найден — флаг -buf")
		return
	case err != nil:
		p.Errorf("buf lint: %v", err)
		return
	case res.Failed:
		p.Errorf("buf lint: не смог проверить пример\n%s", indent(res.Output))
		return
	}
	for _, rule := range l.Meta.Buf.ExpectLint {
		if !strings.Contains(res.Output, " "+rule+" ") {
			p.Errorf("buf lint: нет нарушения %s в выводе\n%s", rule, indent(res.Output))
		}
	}
	if len(l.Meta.Buf.ExpectLint) == 0 && res.Found {
		p.Errorf("buf lint: контракт примера не проходит lint, а buf.expectLint пуст\n%s", indent(res.Output))
	}
}

// checkSqlc требует, чтобы сгенерированный код примера совпадал с тем,
// что выдаёт sqlc по его sqlc.yaml, схеме и запросам: правка запроса
// без перегенерации или код «под sqlc», написанный руками, не пройдут.
func (c *Checker) checkSqlc(ctx context.Context, l *Lesson, p *Problems) {
	if c.Sqlc == "" {
		p.Warnf("sqlc: не найден — код примера не сверен (go install github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 или флаг -sqlc)")
		return
	}
	files, err := exampleFiles(l)
	if err != nil {
		return // уже отмечено в checkExample
	}
	dir, err := os.MkdirTemp("", "sqlc-*")
	if err != nil {
		p.Errorf("sqlc: %v", err)
		return
	}
	defer os.RemoveAll(dir)
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644); err != nil {
			p.Errorf("sqlc: %v", err)
			return
		}
	}
	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	cmd := osexec.CommandContext(ctx, c.Sqlc, "diff")
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		p.Errorf("sqlc diff: сгенерированный код не совпадает с sqlc generate (%v)\n%s", err, indent(string(out)))
	}
}

// liveBuilder — имя построителя шагов из viz.ts: builder: "select".
var liveBuilder = regexp.MustCompile(`builder:\s*"(\w+)"`)

// checkLive запускает программу живой визуализации так же, как runner
// (/api/trace или /api/race), и проверяет, что построителю есть что показать.
func (c *Checker) checkLive(ctx context.Context, l *Lesson, p *Problems) {
	src, err := os.ReadFile(filepath.Join(l.Dir, "live", "main.go"))
	if err != nil {
		return
	}
	viz, err := os.ReadFile(filepath.Join(l.Dir, "viz-live.ts"))
	if err != nil {
		viz, _ = os.ReadFile(filepath.Join(l.Dir, "viz.ts"))
	}
	m := liveBuilder.FindSubmatch(viz)
	if m == nil {
		p.Errorf("viz.ts: у живого визуализатора не найден builder")
		return
	}
	files := sandbox.Files{"main.go": string(src)}
	builder := string(m[1])
	if builder == "race" {
		res, err := livetrace.RunRace(ctx, c.Exec, files, l.Meta.Runner)
		switch {
		case err != nil:
			p.Errorf("live: %v", err)
		case res.BuildFailed:
			p.Errorf("live: не компилируется\n%s", indent(res.BuildOutput))
		case res.TimedOut:
			p.Errorf("live: таймаут")
		case len(res.Races) == 0:
			p.Errorf("live: детектор не нашёл гонок — визуализатору нечего показать\n%s", indent(res.Stderr))
		}
		return
	}
	res, err := livetrace.Run(ctx, c.Exec, files, l.Meta.Runner)
	switch {
	case err != nil:
		p.Errorf("live: %v", err)
	case res.BuildFailed:
		p.Errorf("live: не компилируется\n%s", indent(res.BuildOutput))
	case res.TimedOut || res.ExitCode != 0:
		p.Errorf("live: exit=%d timedOut=%v\n%s", res.ExitCode, res.TimedOut, indent(res.Stderr))
	case res.Trace == nil:
		p.Errorf("live: нет трассы: %s", res.TraceError)
	case builder == "select" && (len(res.Trace.Selects) == 0 || res.Trace.Total == 0):
		p.Errorf("live: в трассе нет событий select — визуализатору нечего показать")
	case builder == "goroutines" && len(res.Trace.Goroutines) < 2:
		p.Errorf("live: в трассе одна горутина — визуализатору нечего показать")
	case builder == "spans" && len(res.Trace.Spans) == 0:
		p.Errorf("live: в трассе нет задач и регионов runtime/trace — визуализатору нечего показать")
	case builder != "select" && builder != "goroutines" && builder != "spans":
		p.Errorf("viz.ts: неизвестный builder %q", builder)
	case res.Trace.Total > livetrace.MaxEvents:
		p.Warnf("live: %d событий, показаны будут первые %d — сократите программу", res.Trace.Total, livetrace.MaxEvents)
	}
}

// checkVulncheck прогоняет govulncheck по примеру, как кнопка в песочнице,
// и требует найти перечисленные в frontmatter уязвимости. Новые записи
// в базе вывод меняют, поэтому сверяются только id, а не весь текст.
func (c *Checker) checkVulncheck(ctx context.Context, l *Lesson, p *Problems) {
	files, err := exampleFiles(l)
	if err != nil {
		return // уже отмечено в checkExample
	}
	res, err := c.Exec.Vulncheck(ctx, files)
	switch {
	case errors.Is(err, sandbox.ErrNoVulncheck):
		p.Errorf("govulncheck: не найден — go install golang.org/x/vuln/cmd/govulncheck@v1.9.0 или флаг -govulncheck")
		return
	case err != nil:
		p.Errorf("govulncheck: %v", err)
		return
	case res.Failed:
		p.Errorf("govulncheck: не смог проверить пример (exit=%d)\n%s", res.ExitCode, indent(res.Output))
		return
	}
	for _, id := range l.Meta.Govulncheck.Expect {
		if !res.Found || !strings.Contains(res.Output, id+"\n") {
			p.Errorf("govulncheck: не нашёл в вызовах примера %s\n%s", id, indent(res.Output))
		}
	}
}

// checkFormat проверяет, что все .go-файлы урока отформатированы gofmt.
func (c *Checker) checkFormat(l *Lesson, p *Problems) {
	files, _ := filepath.Glob(filepath.Join(l.Dir, "*", "*.go"))
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			p.Errorf("%v", err)
			continue
		}
		formatted, err := format.Source(src)
		rel, _ := filepath.Rel(l.Dir, f)
		if err != nil {
			p.Errorf("%s: не разбирается: %v", rel, err)
		} else if !bytes.Equal(src, formatted) {
			p.Errorf("%s: не отформатирован, запустите gofmt -w", rel)
		}
	}
}

func (c *Checker) checkExample(ctx context.Context, l *Lesson, p *Problems) {
	files, err := exampleFiles(l)
	if err != nil {
		p.Errorf("example: %v", err)
		return
	}
	if _, ok := files["main.go"]; !ok {
		return // отсутствие файла уже отмечено при загрузке
	}
	if missing := c.missingLabs(l); len(missing) > 0 {
		// Вывод примера получен на настоящей базе — без неё проверяем только сборку.
		ok, out, err := c.Exec.Build(ctx, files, l.Meta.Runner)
		switch {
		case err != nil:
			p.Errorf("example: %v", err)
		case !ok:
			p.Errorf("example: не компилируется\n%s", indent(out))
		default:
			p.Warnf("example: не запущен, нужен labs %s (флаги -postgres/-mysql)", strings.Join(missing, ", "))
		}
		return
	}
	var res *sandbox.RunResult
	testMode := hasTests(files)
	if testMode {
		var tr *sandbox.GoTestResult
		tr, err = c.Exec.GoTest(ctx, files, l.Meta.Runner, sandbox.TestOptions{Mode: sandbox.ModeTest})
		if tr != nil {
			res = &tr.RunResult
			// Время тестов от запуска к запуску разное — в output.txt его нет.
			res.Stdout = maskDurations(res.Stdout)
		}
	} else {
		res, err = c.Exec.Run(ctx, files, l.Meta.Runner)
	}
	if err != nil {
		p.Errorf("example: %v", err)
		return
	}
	switch {
	case res.BuildFailed:
		p.Errorf("example: не компилируется\n%s", indent(res.BuildOutput))
		return
	case res.TimedOut || res.Truncated || res.ExitCode != 0:
		p.Errorf("example: exit=%d timedOut=%v truncated=%v\n%s%s", res.ExitCode, res.TimedOut, res.Truncated, indent(res.Stdout), indent(res.Stderr))
		return
	}
	if testMode {
		c.checkTestModes(ctx, l, files, p)
	}
	outPath := filepath.Join(l.Dir, "example", "output.txt")
	if c.Update {
		if err := os.WriteFile(outPath, []byte(res.Stdout), 0o644); err != nil {
			p.Errorf("example: %v", err)
		}
		return
	}
	want, err := os.ReadFile(outPath)
	if err != nil {
		return
	}
	if normalize(string(want)) != normalize(res.Stdout) {
		p.Errorf("example: вывод не совпадает с output.txt\n--- ожидалось\n%s--- получено\n%s", indent(string(want)), indent(res.Stdout))
	}
}

// missingLabs — базы профиля labs, которые нужны уроку, но не переданы lessonlint.
func (c *Checker) missingLabs(l *Lesson) []string {
	var missing []string
	for _, name := range l.Meta.Runner.Labs {
		if !c.Exec.HasLab(name) {
			missing = append(missing, name)
		}
	}
	return missing
}

// hasTests — пример с _test.go-файлами запускается через go test, а не go run.
func hasTests(files sandbox.Files) bool {
	for name := range files {
		if strings.HasSuffix(name, "_test.go") {
			return true
		}
	}
	return false
}

var (
	testDuration = regexp.MustCompile(`\(\d+\.\d+s\)`)
	benchFunc    = regexp.MustCompile(`(?m)^func (Benchmark\w*)\(b \*testing\.B\)`)
	fuzzFunc     = regexp.MustCompile(`(?m)^func (Fuzz\w*)\(f \*testing\.F\)`)
)

// maskDurations заменяет время тестов «(0.12s)» на «(0.00s)».
func maskDurations(s string) string { return testDuration.ReplaceAllString(s, "(0.00s)") }

// checkTestModes прогоняет кнопки песочницы, которые увидит ученик:
// покрытие всегда, бенчмарки и fuzzing — если в тестах они есть.
// Сайт находит Benchmark и Fuzz теми же регулярными выражениями.
func (c *Checker) checkTestModes(ctx context.Context, l *Lesson, files sandbox.Files, p *Problems) {
	var tests strings.Builder
	for name, src := range files {
		if strings.HasSuffix(name, "_test.go") {
			tests.WriteString(src)
		}
	}
	type mode struct {
		opts sandbox.TestOptions
		// mayFail — fuzzing как раз и ищет падения, пример может их показывать.
		mayFail bool
	}
	modes := []mode{{opts: sandbox.TestOptions{Mode: sandbox.ModeCover}}}
	if benchFunc.MatchString(tests.String()) {
		modes = append(modes, mode{opts: sandbox.TestOptions{Mode: sandbox.ModeBench}})
	}
	for _, m := range fuzzFunc.FindAllStringSubmatch(tests.String(), -1) {
		modes = append(modes, mode{opts: sandbox.TestOptions{Mode: sandbox.ModeFuzz, Fuzz: m[1], FuzzTime: time.Second}, mayFail: true})
	}
	for _, m := range modes {
		where := "example " + m.opts.Mode
		if m.opts.Fuzz != "" {
			where += " " + m.opts.Fuzz
		}
		res, err := c.Exec.GoTest(ctx, files, l.Meta.Runner, m.opts)
		switch {
		case err != nil:
			p.Errorf("%s: %v", where, err)
		case res.BuildFailed:
			p.Errorf("%s: не компилируется\n%s", where, indent(res.BuildOutput))
		case res.TimedOut || res.Truncated:
			p.Errorf("%s: timedOut=%v truncated=%v", where, res.TimedOut, res.Truncated)
		case res.ExitCode != 0 && !m.mayFail:
			p.Errorf("%s: exit=%d\n%s%s", where, res.ExitCode, indent(res.Stdout), indent(res.Stderr))
		case m.opts.Mode == sandbox.ModeCover && res.Coverage == "":
			p.Errorf("%s: нет отчёта о покрытии", where)
		}
	}
}

// exampleFiles — main.go и файлы рядом с ним (данные для embed и т.п.),
// всё, кроме output.txt. Сайт отправляет в runner тот же набор.
func exampleFiles(l *Lesson) (sandbox.Files, error) {
	entries, err := os.ReadDir(filepath.Join(l.Dir, "example"))
	if err != nil {
		return nil, err
	}
	files := sandbox.Files{}
	for _, e := range entries {
		if e.IsDir() || e.Name() == "output.txt" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(l.Dir, "example", e.Name()))
		if err != nil {
			return nil, err
		}
		files[e.Name()] = string(b)
	}
	return files, files.Validate()
}

func (c *Checker) checkTask(ctx context.Context, l *Lesson, p *Problems) {
	files := sandbox.Files{}
	for _, name := range []string{"task_test.go", "hidden_test.go", "solution.go", "task.go"} {
		b, err := os.ReadFile(filepath.Join(l.Dir, "task", name))
		if err != nil {
			return // отсутствие файла уже отмечено при загрузке
		}
		files[name] = string(b)
	}
	// Остальное в task/ (кроме подсказок) — файлы урока, как у runner:
	// контракты .proto и сгенерированный по ним код.
	lessonFiles, err := taskFiles(l)
	if err != nil {
		p.Errorf("task: %v", err)
		return
	}
	// Шаблон и эталон объявляют одно и то же — собираем их по очереди.
	withTests := func(code string) sandbox.Files {
		out := sandbox.Files{"task.go": code}
		for name, src := range lessonFiles {
			out[name] = src
		}
		return out
	}

	// Эталон проходит все тесты, включая скрытые.
	res, err := c.Exec.Test(ctx, withTests(files["solution.go"]), l.Meta.Runner)
	switch {
	case err != nil:
		p.Errorf("task: %v", err)
	case res.BuildFailed:
		p.Errorf("task: solution.go не компилируется с тестами\n%s", indent(res.BuildOutput))
	case !res.Passed:
		p.Errorf("task: solution.go не проходит тесты: %s", describeFailures(res))
	}

	// Шаблон компилируется, но тесты падают.
	res, err = c.Exec.Test(ctx, withTests(files["task.go"]), l.Meta.Runner)
	switch {
	case err != nil:
		p.Errorf("task: %v", err)
	case res.BuildFailed:
		p.Errorf("task: шаблон task.go не компилируется с тестами\n%s", indent(res.BuildOutput))
	case res.Passed:
		p.Errorf("task: шаблон task.go проходит все тесты — задача решена заранее")
	default:
		checkExploits(l, files, res, p)
	}
}

// taskFiles — файлы урока из task/: всё, кроме task.go, solution.go и hints.md.
func taskFiles(l *Lesson) (sandbox.Files, error) {
	entries, err := os.ReadDir(filepath.Join(l.Dir, "task"))
	if err != nil {
		return nil, err
	}
	files := sandbox.Files{}
	for _, e := range entries {
		switch name := e.Name(); {
		case e.IsDir(), name == "task.go", name == "solution.go", name == "hints.md":
		default:
			b, err := os.ReadFile(filepath.Join(l.Dir, "task", name))
			if err != nil {
				return nil, err
			}
			files[name] = string(b)
		}
	}
	return files, nil
}

var (
	exploitFunc = regexp.MustCompile(`(?m)^func (TestExploit\w*)\(t \*testing\.T\)`)
	reviewFunc  = regexp.MustCompile(`(?m)^func (TestReview\w*)\(t \*testing\.T\)`)
)

// checkExploits проверяет задачи по безопасности и ревью: каждая проверка
// (TestExploit… или TestReview… в hidden_test.go) срабатывает на шаблоне —
// тест падает, — а на эталоне нет (это уже проверено: эталон проходит всё).
// В задачах «найди и почини» (taskKind: fix) и «приведи к виду для ревью»
// (taskKind: review) шаблон вдобавок проходит все остальные тесты: код
// работает, его надо исправить, а не дописать.
func checkExploits(l *Lesson, files sandbox.Files, res *sandbox.TestResult, p *Problems) {
	re, what := exploitFunc, "эксплойт"
	if l.Meta.TaskKind == "review" {
		re, what = reviewFunc, "проверка ревью"
	}
	checks := map[string]bool{}
	for _, m := range re.FindAllStringSubmatch(files["hidden_test.go"], -1) {
		checks[m[1]] = true
	}
	switch {
	case l.Meta.TaskKind == "fix" && len(checks) == 0:
		p.Errorf("task: в задаче «найди и почини» нет эксплойтов func TestExploitXxx(t *testing.T) в hidden_test.go")
	case l.Meta.TaskKind == "review" && len(checks) == 0:
		p.Errorf("task: в задаче «приведи к виду для ревью» нет проверок func TestReviewXxx(t *testing.T) в hidden_test.go")
	}
	if len(checks) == 0 {
		return
	}
	status := map[string]string{}
	for _, tc := range res.Tests {
		if !strings.Contains(tc.Name, "/") {
			status[tc.Name] = tc.Status
		}
	}
	for name := range checks {
		switch status[name] {
		case "fail":
		case "":
			p.Errorf("task: %s %s не запускался на шаблоне — паника в другом тесте?%s", what, name, describeFailures(res))
		default:
			p.Errorf("task: %s %s не срабатывает на шаблоне (%s)", what, name, status[name])
		}
	}
	if l.Meta.TaskKind == "" {
		return
	}
	for _, src := range []string{files["task_test.go"], files["hidden_test.go"]} {
		for _, m := range testFunc.FindAllStringSubmatch(src, -1) {
			if name := m[1]; !checks[name] && status[name] != "pass" && status[name] != "skip" {
				p.Errorf("task: шаблон должен проходить функциональный тест %s, а он %q%s", name, status[name], describeFailures(res))
			}
		}
	}
}

var testFunc = regexp.MustCompile(`(?m)^func (Test\w*)\(t \*testing\.T\)`)

func describeFailures(res *sandbox.TestResult) string {
	var b strings.Builder
	for _, tc := range res.Tests {
		if tc.Status == "fail" {
			b.WriteString("\n    " + tc.Name + "\n" + indent(tc.Output))
		}
	}
	if res.TimedOut {
		b.WriteString("\n    таймаут")
	}
	if res.Output != "" {
		b.WriteString("\n" + indent(res.Output))
	}
	return b.String()
}

func (c *Checker) checkQuiz(ctx context.Context, l *Lesson, p *Problems) {
	for _, q := range l.Quiz.Questions {
		where := "quiz: " + q.ID
		files := sandbox.Files{"main.go": q.Code}
		switch q.Kind {
		case "output":
			res, err := c.Exec.Run(ctx, files, sandbox.Flags{})
			if err != nil {
				p.Errorf("%s: %v", where, err)
				continue
			}
			if res.BuildFailed || res.TimedOut {
				p.Errorf("%s: программа не запускается\n%s", where, indent(res.BuildOutput+res.Stderr))
				continue
			}
			got := strings.TrimSpace(res.Stdout)
			for _, o := range q.Options {
				if o.Correct && strings.TrimSpace(o.Text) != got {
					p.Errorf("%s: правильный вариант %q, а программа выводит %q", where, o.Text, got)
				}
			}
		case "compile":
			if q.Compiles == nil {
				continue
			}
			ok, _, err := c.Exec.Build(ctx, files, sandbox.Flags{})
			if err != nil {
				p.Errorf("%s: %v", where, err)
			} else if ok != *q.Compiles {
				p.Errorf("%s: compiles: %v, а go build говорит %v", where, *q.Compiles, ok)
			}
		}
	}
}

func normalize(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

func indent(s string) string {
	s = strings.TrimRight(s, "\n")
	if s == "" {
		return ""
	}
	return "    " + strings.ReplaceAll(s, "\n", "\n    ") + "\n"
}
