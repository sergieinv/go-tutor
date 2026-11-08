package main

import (
	"bytes"
	"context"
	"errors"
	"go/format"
	"os"
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
	viz, _ := os.ReadFile(filepath.Join(l.Dir, "viz.ts"))
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
	case builder != "select" && builder != "goroutines":
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
	default:
		checkExploits(l, files, res, p)
	}
}

var exploitFunc = regexp.MustCompile(`(?m)^func (TestExploit\w*)\(t \*testing\.T\)`)

// checkExploits проверяет задачи по безопасности: каждый эксплойт
// (TestExploit… в hidden_test.go) срабатывает на шаблоне — тест падает, —
// а на эталоне не срабатывает (это уже проверено: эталон проходит всё).
// В задаче «найди и почини» (taskKind: fix) шаблон вдобавок проходит все
// остальные тесты: уязвимый код работает, его надо починить, а не дописать.
func checkExploits(l *Lesson, files sandbox.Files, res *sandbox.TestResult, p *Problems) {
	exploits := map[string]bool{}
	for _, m := range exploitFunc.FindAllStringSubmatch(files["hidden_test.go"], -1) {
		exploits[m[1]] = true
	}
	if l.Meta.TaskKind == "fix" && len(exploits) == 0 {
		p.Errorf("task: в задаче «найди и почини» нет эксплойтов func TestExploitXxx(t *testing.T) в hidden_test.go")
	}
	if len(exploits) == 0 {
		return
	}
	status := map[string]string{}
	for _, tc := range res.Tests {
		if !strings.Contains(tc.Name, "/") {
			status[tc.Name] = tc.Status
		}
	}
	for name := range exploits {
		switch status[name] {
		case "fail":
		case "":
			p.Errorf("task: эксплойт %s не запускался на шаблоне — паника в другом тесте?%s", name, describeFailures(res))
		default:
			p.Errorf("task: эксплойт %s не срабатывает на шаблоне (%s)", name, status[name])
		}
	}
	if l.Meta.TaskKind != "fix" {
		return
	}
	for _, src := range []string{files["task_test.go"], files["hidden_test.go"]} {
		for _, m := range testFunc.FindAllStringSubmatch(src, -1) {
			if name := m[1]; !exploits[name] && status[name] != "pass" {
				p.Errorf("task: шаблон «найди и почини» должен проходить функциональный тест %s, а он %q%s", name, status[name], describeFailures(res))
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
