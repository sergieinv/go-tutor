package livetrace

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"gotutor/runner/sandbox"
)

const selectProgram = `package main

import (
	"fmt"
	"time"
)

func main() {
	a := make(chan string)
	go func() {
		time.Sleep(5 * time.Millisecond)
		a <- "a"
	}()
loop:
	select {
	case v := <-a:
		fmt.Println(v)
	case <-time.After(time.Second):
		break loop
	}
	b := make(chan int, 1)
	b <- 1
	select {
	case v := <-b:
		fmt.Println(v)
	default:
	}
}
`

func TestInstrumentKeepsLines(t *testing.T) {
	files, selects, err := Instrument(sandbox.Files{"main.go": selectProgram})
	if err != nil {
		t.Fatal(err)
	}
	got := files["main.go"]
	if a, b := strings.Count(got, "\n"), strings.Count(selectProgram, "\n"); a != b {
		t.Fatalf("строк стало %d, было %d", a, b)
	}
	for _, want := range []string{
		"func gotutorMain() {",
		"gotutorSelect(1); loop:",
		"case v := <-a: gotutorCase(1, 0);",
		"case <-time.After(time.Second): gotutorCase(1, 1);",
		"gotutorSelect(2); select {",
		"default: gotutorCase(2, 1);",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("нет %q в\n%s", want, got)
		}
	}
	if _, ok := files[generatedFile]; !ok {
		t.Error("нет сгенерированного main")
	}
	if len(selects) != 2 || selects[0].Line != 15 || selects[1].Line != 23 {
		t.Fatalf("selects = %+v", selects)
	}
	c := selects[0].Cases
	if len(c) != 2 || c[0].Text != "case v := <-a" || c[0].Kind != "recv" || c[0].Line != 16 || c[1].Line != 18 {
		t.Errorf("cases = %+v", c)
	}
	if k := selects[1].Cases[1].Kind; k != "default" {
		t.Errorf("kind = %s", k)
	}
}

func TestInstrumentNeedsMain(t *testing.T) {
	_, _, err := Instrument(sandbox.Files{"lib.go": "package lib\n"})
	if err == nil {
		t.Fatal("ждали ошибку без main")
	}
	_, _, err = Instrument(sandbox.Files{"main.go": "package main\nfunc main(){}\n", "gotutor_x.go": "package main\n"})
	if err == nil {
		t.Fatal("ждали ошибку на зарезервированном имени")
	}
}

func newExecutor(t *testing.T) *sandbox.Executor {
	t.Helper()
	if _, err := exec.LookPath("go"); err != nil {
		t.Skip("нет go")
	}
	cfg := sandbox.DefaultConfig()
	cfg.WorkRoot = t.TempDir()
	e, err := sandbox.New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func TestRunSelect(t *testing.T) {
	e := newExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	res, err := Run(ctx, e, sandbox.Files{"main.go": selectProgram}, sandbox.Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.ExitCode != 0 {
		t.Fatalf("запуск: %+v", res.RunResult)
	}
	if res.Trace == nil {
		t.Fatalf("нет трассы: %s", res.TraceError)
	}
	tr := res.Trace
	if len(tr.Goroutines) != 2 || tr.Goroutines[0].Func != "main.main" || tr.Goroutines[1].Line != 10 {
		t.Fatalf("goroutines = %+v", tr.Goroutines)
	}
	worker := tr.Goroutines[1].ID
	main := tr.Goroutines[0].ID
	if got := tr.Selects[0].Counts; got[0] != 1 || got[1] != 0 {
		t.Errorf("counts[0] = %v", got)
	}
	if got := tr.Selects[1].Counts; got[0] != 1 || got[1] != 0 {
		t.Errorf("counts[1] = %v", got)
	}
	// main ждёт в select, рабочая горутина будит его отправкой со строки 12.
	var kinds []string
	var blocked, woken bool
	for _, ev := range tr.Events {
		kinds = append(kinds, ev.Kind)
		if ev.Kind == "block" && ev.G == main && ev.Reason == "select" && ev.Line == 15 {
			blocked = true
		}
		if ev.Kind == "unblock" && ev.Target == main && ev.G == worker && ev.Line == 12 {
			woken = true
		}
	}
	if !blocked || !woken {
		t.Errorf("blocked=%v woken=%v, события: %v\n%+v", blocked, woken, kinds, tr.Events)
	}
	if res.Stdout != "a\n1\n" {
		t.Errorf("stdout = %q", res.Stdout)
	}
}

func TestRunWithoutTrace(t *testing.T) {
	e := newExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	src := "package main\n\nimport \"os\"\n\nfunc main() {\n\tos.Exit(3)\n}\n"
	res, err := Run(ctx, e, sandbox.Files{"main.go": src}, sandbox.Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 3 || res.Trace != nil || res.TraceError == "" {
		t.Fatalf("res = %+v trace=%v err=%q", res.RunResult, res.Trace, res.TraceError)
	}
}

// TestWriteFixture обновляет образцы ответов runner для тестов построителей шагов
// на TypeScript: GOTUTOR_FIXTURE=1 go test ./livetrace -run WriteFixture
func TestWriteFixture(t *testing.T) {
	if os.Getenv("GOTUTOR_FIXTURE") == "" {
		t.Skip("GOTUTOR_FIXTURE не задан")
	}
	e := newExecutor(t)
	read := func(path string) sandbox.Files {
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		return sandbox.Files{"main.go": string(src)}
	}
	write := func(name string, v any) {
		out, _ := json.MarshalIndent(v, "", "  ")
		if err := os.WriteFile("../../viz/test/testdata/"+name, append(out, '\n'), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for name, src := range map[string]string{
		"select-trace.json":   "../../content/05-concurrency/07-select/live/main.go",
		"pipeline-trace.json": "testdata/pipeline.go",
		"spans-trace.json":    "testdata/spans.go",
	} {
		res, err := Run(context.Background(), e, read(src), sandbox.Flags{})
		if err != nil || res.Trace == nil {
			t.Fatalf("%s: err=%v res=%+v", name, err, res)
		}
		write(name, res)
	}
	res, err := RunRace(context.Background(), e, read("testdata/race.go"), sandbox.Flags{})
	if err != nil || len(res.Races) == 0 {
		t.Fatalf("race: err=%v res=%+v", err, res)
	}
	write("race.json", res)
}

// Горутины из wg.Go стартуют в пакете sync, но это горутины ученика.
func TestRunWaitGroupGo(t *testing.T) {
	e := newExecutor(t)
	src := "package main\n\nimport \"sync\"\n\nfunc main() {\n\tvar wg sync.WaitGroup\n\tch := make(chan int)\n\twg.Go(func() { ch <- 1 })\n\twg.Go(func() { <-ch })\n\twg.Wait()\n}\n"
	res, err := Run(context.Background(), e, sandbox.Files{"main.go": src}, sandbox.Flags{})
	if err != nil || res.Trace == nil {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	g := res.Trace.Goroutines
	if len(g) != 3 || g[1].Line != 8 || g[2].Line != 9 || !strings.Contains(g[1].Func, "WaitGroup") {
		t.Fatalf("goroutines = %+v", g)
	}
}

// Задачи, регионы и логи runtime/trace попадают в Spans и Logs.
func TestRunSpans(t *testing.T) {
	e := newExecutor(t)
	src, err := os.ReadFile("testdata/spans.go")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), e, sandbox.Files{"main.go": string(src)}, sandbox.Flags{})
	if err != nil || res.Trace == nil {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	var tasks, regions []Span
	for _, s := range res.Trace.Spans {
		if s.Kind == "task" {
			tasks = append(tasks, s)
		} else {
			regions = append(regions, s)
		}
	}
	if len(tasks) != 3 || tasks[0].Name != "gateway" || tasks[1].Task != tasks[0].ID || tasks[2].Task != tasks[1].ID {
		t.Fatalf("tasks = %+v", tasks)
	}
	if len(regions) != 3 || regions[2].Task != tasks[2].ID || regions[0].End <= regions[0].Start {
		t.Fatalf("regions = %+v", regions)
	}
	var cats []string
	for _, l := range res.Trace.Logs {
		cats = append(cats, l.Category)
	}
	if strings.Join(cats, ",") != "deadline,deadline,deadline,error" || res.Trace.Logs[3].Task != tasks[2].ID {
		t.Fatalf("logs = %+v", res.Trace.Logs)
	}
}
