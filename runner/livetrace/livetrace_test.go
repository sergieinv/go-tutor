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

// TestWriteFixture обновляет трассу-образец для тестов построителя шагов
// на TypeScript: GOTUTOR_FIXTURE=1 go test ./livetrace -run WriteFixture
func TestWriteFixture(t *testing.T) {
	if os.Getenv("GOTUTOR_FIXTURE") == "" {
		t.Skip("GOTUTOR_FIXTURE не задан")
	}
	src, err := os.ReadFile("../../content/05-concurrency/07-select/live/main.go")
	if err != nil {
		t.Fatal(err)
	}
	res, err := Run(context.Background(), newExecutor(t), sandbox.Files{"main.go": string(src)}, sandbox.Flags{})
	if err != nil || res.Trace == nil {
		t.Fatalf("err=%v res=%+v", err, res)
	}
	out, _ := json.MarshalIndent(res, "", "  ")
	if err := os.WriteFile("../../viz/test/testdata/select-trace.json", append(out, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
}
