package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"go/format"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// goVersion — версия языка в go.mod, который lessonlint создаёт для запусков.
// Должна совпадать с версией в runner.
const goVersion = "1.25"

const runTimeout = 60 * time.Second

type Checker struct {
	Update bool // перезаписать example/output.txt реальным выводом
}

func (c *Checker) CheckLesson(ctx context.Context, l *Lesson, p *Problems) {
	c.checkFormat(l, p)
	c.checkExample(ctx, l, p)
	if l.HasTask {
		c.checkTask(ctx, l, p)
	}
	c.checkQuiz(ctx, l, p)
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
	src, err := os.ReadFile(filepath.Join(l.Dir, "example", "main.go"))
	if err != nil {
		return // отсутствие файла уже отмечено при загрузке
	}
	res := goRun(ctx, l.Meta.Runner, map[string][]byte{"main.go": src})
	if res.err != nil {
		p.Errorf("example: go run завершился ошибкой: %v\n%s", res.err, indent(res.stderr))
		return
	}
	outPath := filepath.Join(l.Dir, "example", "output.txt")
	if c.Update {
		if err := os.WriteFile(outPath, res.stdout, 0o644); err != nil {
			p.Errorf("example: %v", err)
		}
		return
	}
	want, err := os.ReadFile(outPath)
	if err != nil {
		return
	}
	if normalize(want) != normalize(res.stdout) {
		p.Errorf("example: вывод не совпадает с output.txt\n--- ожидалось\n%s--- получено\n%s", indent(want), indent(res.stdout))
	}
}

func (c *Checker) checkTask(ctx context.Context, l *Lesson, p *Problems) {
	read := func(name string) []byte {
		b, err := os.ReadFile(filepath.Join(l.Dir, "task", name))
		if err != nil {
			p.Errorf("task/%s: %v", name, err)
		}
		return b
	}
	tests := map[string][]byte{
		"task_test.go":   read("task_test.go"),
		"hidden_test.go": read("hidden_test.go"),
	}
	solution, template := read("solution.go"), read("task.go")
	if len(p.Errors) > 0 && (solution == nil || template == nil) {
		return
	}

	withTests := func(name string, src []byte) map[string][]byte {
		files := map[string][]byte{name: src}
		for k, v := range tests {
			files[k] = v
		}
		return files
	}

	// Эталон проходит все тесты, включая скрытые.
	res := goTest(ctx, l.Meta.Runner, withTests("solution.go", solution))
	if res.buildFailed {
		p.Errorf("task: solution.go не компилируется с тестами\n%s", indent(res.output))
	} else if len(res.failed) > 0 || !res.passed {
		p.Errorf("task: solution.go не проходит тесты: %s\n%s", strings.Join(res.failed, ", "), indent(res.output))
	}

	// Шаблон компилируется, но тесты падают.
	res = goTest(ctx, l.Meta.Runner, withTests("task.go", template))
	if res.buildFailed {
		p.Errorf("task: шаблон task.go не компилируется с тестами\n%s", indent(res.output))
	} else if res.passed {
		p.Errorf("task: шаблон task.go проходит все тесты — задача решена заранее")
	}
}

func (c *Checker) checkQuiz(ctx context.Context, l *Lesson, p *Problems) {
	for _, q := range l.Quiz.Questions {
		where := "quiz: " + q.ID
		switch q.Kind {
		case "output":
			res := goRun(ctx, RunnerFlags{}, map[string][]byte{"main.go": []byte(q.Code)})
			if res.err != nil {
				p.Errorf("%s: программа не запускается: %v\n%s", where, res.err, indent(res.stderr))
				continue
			}
			got := strings.TrimSpace(string(res.stdout))
			for _, o := range q.Options {
				if o.Correct && strings.TrimSpace(o.Text) != got {
					p.Errorf("%s: правильный вариант %q, а программа выводит %q", where, o.Text, got)
				}
			}
		case "compile":
			if q.Compiles == nil {
				continue
			}
			ok := goBuild(ctx, map[string][]byte{"main.go": []byte(q.Code)})
			if ok != *q.Compiles {
				p.Errorf("%s: compiles: %v, а go build говорит %v", where, *q.Compiles, ok)
			}
		}
	}
}

type runResult struct {
	stdout, stderr []byte
	err            error
}

func goRun(ctx context.Context, flags RunnerFlags, files map[string][]byte) runResult {
	dir, cleanup, err := tempModule(files)
	if err != nil {
		return runResult{err: err}
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	args := []string{"run"}
	if flags.Race {
		args = append(args, "-race")
	}
	args = append(args, ".")
	cmd := goCmd(ctx, dir, flags, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	return runResult{stdout: stdout.Bytes(), stderr: stderr.Bytes(), err: err}
}

func goBuild(ctx context.Context, files map[string][]byte) bool {
	dir, cleanup, err := tempModule(files)
	if err != nil {
		return false
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	return goCmd(ctx, dir, RunnerFlags{}, "build", "-o", os.DevNull, ".").Run() == nil
}

type testResult struct {
	passed      bool // пакет целиком прошёл
	buildFailed bool
	failed      []string // имена упавших тестов
	output      []byte   // вывод упавших тестов или ошибки сборки
}

// goTest запускает go test -json и разбирает события так же, как это будет
// делать фронтенд: тест → ok/fail + вывод.
func goTest(ctx context.Context, flags RunnerFlags, files map[string][]byte) testResult {
	dir, cleanup, err := tempModule(files)
	if err != nil {
		return testResult{buildFailed: true, output: []byte(err.Error())}
	}
	defer cleanup()
	ctx, cancel := context.WithTimeout(ctx, runTimeout)
	defer cancel()
	args := []string{"test", "-json", "-count=1"}
	if flags.Race {
		args = append(args, "-race")
	}
	args = append(args, ".")
	cmd := goCmd(ctx, dir, flags, args...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	_ = cmd.Run() // код возврата не нужен: всё есть в событиях

	var res testResult
	outputs := map[string]*bytes.Buffer{}
	dec := json.NewDecoder(&stdout)
	for dec.More() {
		var ev struct {
			Action string
			Test   string
			Output string
		}
		if err := dec.Decode(&ev); err != nil {
			break
		}
		switch ev.Action {
		case "build-output":
			res.output = append(res.output, ev.Output...)
		case "build-fail":
			res.buildFailed = true
		case "output":
			if outputs[ev.Test] == nil {
				outputs[ev.Test] = &bytes.Buffer{}
			}
			outputs[ev.Test].WriteString(ev.Output)
		case "pass":
			if ev.Test == "" {
				res.passed = true
			}
		case "fail":
			if ev.Test != "" {
				res.failed = append(res.failed, ev.Test)
				res.output = append(res.output, outputs[ev.Test].Bytes()...)
			} else if len(res.failed) == 0 && !res.buildFailed {
				// Пакет упал без упавших тестов: паника при инициализации, таймаут и т.п.
				res.output = append(res.output, outputs[""].Bytes()...)
			}
		}
	}
	if stdout.Len() == 0 && stderr.Len() > 0 {
		res.buildFailed = true
		res.output = append(res.output, stderr.Bytes()...)
	}
	return res
}

// goCmd готовит команду go; вызывающий отвечает за ctx с таймаутом.
func goCmd(ctx context.Context, dir string, flags RunnerFlags, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local", "GOFLAGS=-mod=mod", "GOWORK=off")
	if flags.GOMAXPROCS > 0 {
		cmd.Env = append(cmd.Env, "GOMAXPROCS="+strconv.Itoa(flags.GOMAXPROCS))
	}
	if flags.GODEBUG != "" {
		cmd.Env = append(cmd.Env, "GODEBUG="+flags.GODEBUG)
	}
	return cmd
}

// tempModule создаёт временный модуль с переданными файлами и go.mod.
func tempModule(files map[string][]byte) (string, func(), error) {
	dir, err := os.MkdirTemp("", "lessonlint-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { os.RemoveAll(dir) }
	if _, ok := files["go.mod"]; !ok {
		files["go.mod"] = fmt.Appendf(nil, "module lesson\n\ngo %s\n", goVersion)
	}
	for name, src := range files {
		if err := os.WriteFile(filepath.Join(dir, name), src, 0o644); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return dir, cleanup, nil
}

func normalize(b []byte) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t\r")
	}
	return strings.Join(lines, "\n")
}

func indent(b []byte) string {
	s := strings.TrimRight(string(b), "\n")
	if s == "" {
		return ""
	}
	return "    " + strings.ReplaceAll(s, "\n", "\n    ") + "\n"
}
