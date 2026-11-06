package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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
	ws, err := e.newWorkspace(uid, files)
	if err != nil {
		return nil, err
	}
	defer ws.remove()

	args := []string{"test", "-c", "-o", "pkg.test"}
	if flags.Race {
		args = append(args, "-race")
	}
	if out, err := e.goTool(ctx, ws.dir, flags, append(args, ".")...); err != nil {
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
