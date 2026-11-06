package main

import (
	"bytes"
	"context"
	"go/format"
	"os"
	"path/filepath"
	"strings"

	"gotutor/runner/livetrace"
	"gotutor/runner/sandbox"
)

// Checker прогоняет уроки через ту же песочницу, что и runner: сборка,
// лимиты и разбор go test -json в CI и в браузере совпадают.
type Checker struct {
	Exec   *sandbox.Executor
	Update bool // перезаписать example/output.txt реальным выводом
}

func (c *Checker) CheckLesson(ctx context.Context, l *Lesson, p *Problems) {
	c.checkFormat(l, p)
	c.checkExample(ctx, l, p)
	if l.HasTask {
		c.checkTask(ctx, l, p)
	}
	c.checkQuiz(ctx, l, p)
	if l.HasLive {
		c.checkLive(ctx, l, p)
	}
}

// checkLive запускает программу живой визуализации так же, как /api/trace:
// она должна отработать и оставить трассу с событиями select.
func (c *Checker) checkLive(ctx context.Context, l *Lesson, p *Problems) {
	src, err := os.ReadFile(filepath.Join(l.Dir, "live", "main.go"))
	if err != nil {
		return
	}
	res, err := livetrace.Run(ctx, c.Exec, sandbox.Files{"main.go": string(src)}, l.Meta.Runner)
	switch {
	case err != nil:
		p.Errorf("live: %v", err)
	case res.BuildFailed:
		p.Errorf("live: не компилируется\n%s", indent(res.BuildOutput))
	case res.TimedOut || res.ExitCode != 0:
		p.Errorf("live: exit=%d timedOut=%v\n%s", res.ExitCode, res.TimedOut, indent(res.Stderr))
	case res.Trace == nil:
		p.Errorf("live: нет трассы: %s", res.TraceError)
	case len(res.Trace.Selects) == 0 || res.Trace.Total == 0:
		p.Errorf("live: в трассе нет событий select — визуализатору нечего показать")
	case res.Trace.Total > livetrace.MaxEvents:
		p.Warnf("live: %d событий, показаны будут первые %d — сократите программу", res.Trace.Total, livetrace.MaxEvents)
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
	src, err := os.ReadFile(filepath.Join(l.Dir, "example", "main.go"))
	if err != nil {
		return // отсутствие файла уже отмечено при загрузке
	}
	res, err := c.Exec.Run(ctx, sandbox.Files{"main.go": string(src)}, l.Meta.Runner)
	if err != nil {
		p.Errorf("example: %v", err)
		return
	}
	switch {
	case res.BuildFailed:
		p.Errorf("example: не компилируется\n%s", indent(res.BuildOutput))
		return
	case res.TimedOut || res.Truncated || res.ExitCode != 0:
		p.Errorf("example: exit=%d timedOut=%v truncated=%v\n%s", res.ExitCode, res.TimedOut, res.Truncated, indent(res.Stderr))
		return
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

func (c *Checker) checkTask(ctx context.Context, l *Lesson, p *Problems) {
	files := sandbox.Files{}
	for _, name := range []string{"task_test.go", "hidden_test.go", "solution.go", "task.go"} {
		b, err := os.ReadFile(filepath.Join(l.Dir, "task", name))
		if err != nil {
			return // отсутствие файла уже отмечено при загрузке
		}
		files[name] = string(b)
	}
	// Шаблон и эталон объявляют одно и то же — собираем их по очереди.
	withTests := func(code string) sandbox.Files {
		return sandbox.Files{"task.go": code, "task_test.go": files["task_test.go"], "hidden_test.go": files["hidden_test.go"]}
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
	}
}

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
