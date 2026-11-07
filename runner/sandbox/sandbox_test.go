package sandbox

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Тест-бинарник служит лаунчером сам для себя: так rlimit'ы и смена uid
// проверяются тем же кодом, что и в runner.
func TestMain(m *testing.M) {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "launch":
			if err := Launch(os.Args[2:]); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(126)
			}
		case "killall":
			if err := KillAll(); err != nil {
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
	code := m.Run()
	if launcherDir != "" {
		os.RemoveAll(launcherDir)
	}
	os.Exit(code)
}

var launcherDir string

// testLauncher копирует тест-бинарник туда, где его может запустить uid слота:
// каталог go-build, в котором лежит сам бинарник, закрыт правами 0700.
func testLauncher(t *testing.T) string {
	t.Helper()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if !isolated() {
		return self
	}
	if launcherDir == "" {
		dir, err := os.MkdirTemp("", "launcher-")
		if err != nil {
			t.Fatal(err)
		}
		launcherDir = dir
		src, err := os.ReadFile(self)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "launcher"), src, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return filepath.Join(launcherDir, "launcher")
}

// isolated — запуск под root на Linux: слоты с отдельными uid и NPROC.
func isolated() bool { return runtime.GOOS == "linux" && os.Getuid() == 0 }

func newExecutor(t *testing.T) *Executor {
	t.Helper()
	cfg := DefaultConfig()
	cfg.WallTime = 3 * time.Second
	cfg.CPUTime = 3 * time.Second
	if runtime.GOOS == "linux" {
		cfg.Launcher = testLauncher(t)
	}
	if isolated() {
		cfg.UIDs = []int{20001, 20002}
		// Родитель t.TempDir() закрыт правами 0700 — uid слота туда не войдёт.
		root, err := os.MkdirTemp("", "work-")
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { os.RemoveAll(root) })
		if err := os.Chmod(root, 0o711); err != nil {
			t.Fatal(err)
		}
		cfg.WorkRoot = root
	}
	e, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	return e
}

func program(body string, imports ...string) Files {
	var b strings.Builder
	b.WriteString("package main\n\n")
	for _, imp := range imports {
		fmt.Fprintf(&b, "import %q\n", imp)
	}
	fmt.Fprintf(&b, "\nfunc main() {\n%s\n}\n", body)
	return Files{"main.go": b.String()}
}

func TestRunHello(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Run(context.Background(), program(`fmt.Println("привет")`, "fmt"), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Stdout != "привет\n" || res.ExitCode != 0 {
		t.Fatalf("Run = %+v", res)
	}
}

func TestRunBuildError(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Run(context.Background(), program(`x := 1`), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || !strings.Contains(res.BuildOutput, "declared and not used") {
		t.Fatalf("ожидалась ошибка сборки, got %+v", res)
	}
	if strings.Contains(res.BuildOutput, os.TempDir()) {
		t.Errorf("в выводе сборки остался путь к временному каталогу: %s", res.BuildOutput)
	}
}

func TestRunExitCodeAndStderr(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Run(context.Background(), program(`panic("бум")`), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 2 || !strings.Contains(res.Stderr, "panic: бум") {
		t.Fatalf("Run = %+v", res)
	}
}

func TestRunInfiniteLoopIsKilled(t *testing.T) {
	e := newExecutor(t)
	start := time.Now()
	res, err := e.Run(context.Background(), program(`for {}`), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("бесконечный цикл не помечен как таймаут: %+v", res)
	}
	if d := time.Since(start); d > 10*time.Second {
		t.Errorf("обрыв занял %v", d)
	}
}

func TestRunSleepingProgramIsKilled(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Run(context.Background(), program(`time.Sleep(time.Hour)`, "time"), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.TimedOut {
		t.Fatalf("спящая программа не помечена как таймаут: %+v", res)
	}
}

func TestRunOutputLimit(t *testing.T) {
	e := newExecutor(t)
	start := time.Now()
	res, err := e.Run(context.Background(), program(`for { fmt.Println("спам") }`, "fmt"), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || len(res.Stdout) > DefaultConfig().MaxOutput {
		t.Fatalf("вывод не обрезан: truncated=%v len=%d", res.Truncated, len(res.Stdout))
	}
	if res.TimedOut || time.Since(start) > 2*time.Second {
		t.Errorf("переполнение вывода должно обрывать программу сразу, а не по таймауту")
	}
}

func TestRunMemoryLimit(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("rlimit'ы применяются только на Linux")
	}
	e := newExecutor(t)
	res, err := e.Run(context.Background(), program(`
	var keep [][]byte
	for i := 0; i < 64; i++ {
		b := make([]byte, 64<<20)
		for j := range b { b[j] = 1 }
		keep = append(keep, b)
	}
	fmt.Println(len(keep))`, "fmt"), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 || (res.ExitCode == -1 && !res.TimedOut) {
		t.Fatalf("ожидалось падение по лимиту памяти: %+v", res)
	}
	t.Logf("лимит памяти: exit=%d stderr=%.120s", res.ExitCode, res.Stderr)
}

func TestRunForkBomb(t *testing.T) {
	if !isolated() {
		t.Skip("NPROC и изоляция по uid — только под root на Linux")
	}
	e := newExecutor(t)
	start := time.Now()
	res, err := e.Run(context.Background(), program(`
	for {
		cmd := exec.Command(os.Args[0])
		_ = cmd.Start()
	}`, "os", "os/exec"), Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if time.Since(start) > 10*time.Second {
		t.Errorf("fork-бомба продержалась %v", time.Since(start))
	}
	t.Logf("fork-бомба: exit=%d timedOut=%v stderr=%.200s", res.ExitCode, res.TimedOut, res.Stderr)
	if res.ExitCode == -1 && !res.TimedOut {
		t.Fatalf("программа не запустилась: %s", res.Stderr)
	}
	// После запроса у uid слота не должно остаться процессов.
	time.Sleep(200 * time.Millisecond)
	if n := processesOf(t, 20001) + processesOf(t, 20002); n > 0 {
		t.Errorf("после запуска остались процессы слота: %d", n)
	}
}

func processesOf(t *testing.T, uid int) int {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range entries {
		status, err := os.ReadFile("/proc/" + e.Name() + "/status")
		if err != nil {
			continue
		}
		zombie, owned := false, false
		for _, line := range strings.Split(string(status), "\n") {
			f := strings.Fields(line)
			if len(f) > 1 && f[0] == "State:" && f[1] == "Z" {
				zombie = true
			}
			if len(f) > 1 && f[0] == "Uid:" && f[1] == fmt.Sprint(uid) {
				owned = true
			}
		}
		// Зомби уже мертвы — их соберёт init контейнера.
		if owned && !zombie {
			n++
		}
	}
	return n
}

func TestRunRejectsBadInput(t *testing.T) {
	e := newExecutor(t)
	for name, files := range map[string]Files{
		"path traversal": {"../main.go": "package main"},
		"cgo":            {"main.go": "package main\n\nimport \"C\"\n\nfunc main() {}\n"},
		"empty":          {},
	} {
		_, err := e.Run(context.Background(), files, Flags{})
		if _, ok := err.(*InputError); !ok {
			t.Errorf("%s: ожидалась InputError, got %v", name, err)
		}
	}
}

const pkgSrc = `package task

func Add(a, b int) int { return a + b }
`

func TestTestReportsEachTest(t *testing.T) {
	e := newExecutor(t)
	files := Files{
		"task.go": pkgSrc,
		"task_test.go": `package task

import "testing"

func TestAddOK(t *testing.T) {
	if Add(1, 2) != 3 { t.Fatal("bad") }
}

func TestAddWrong(t *testing.T) {
	t.Log("подробности")
	if Add(2, 2) != 5 { t.Errorf("Add(2, 2) = %d, want 5", Add(2, 2)) }
}

func TestSub(t *testing.T) {
	t.Run("inner", func(t *testing.T) {})
}
`,
	}
	res, err := e.Test(context.Background(), files, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Passed {
		t.Fatalf("Test = %+v", res)
	}
	got := map[string]TestCase{}
	for _, tc := range res.Tests {
		got[tc.Name] = tc
	}
	if got["TestAddOK"].Status != "pass" || got["TestSub/inner"].Status != "pass" {
		t.Errorf("статусы: %+v", res.Tests)
	}
	wrong := got["TestAddWrong"]
	if wrong.Status != "fail" || !strings.Contains(wrong.Output, "Add(2, 2) = 4, want 5") || !strings.Contains(wrong.Output, "подробности") {
		t.Errorf("TestAddWrong = %+v", wrong)
	}
	if strings.Contains(wrong.Output, "=== RUN") {
		t.Errorf("служебные строки попали в вывод теста: %q", wrong.Output)
	}
}

func TestTestPanicFailsRunningTest(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Test(context.Background(), Files{
		"task.go": pkgSrc,
		"task_test.go": `package task

import "testing"

func TestBoom(t *testing.T) { var m map[string]int; m["x"] = 1 }
`,
	}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Passed || len(res.Tests) != 1 || res.Tests[0].Status != "fail" {
		t.Fatalf("Test = %+v", res)
	}
	if !strings.Contains(res.Tests[0].Output+res.Output, "assignment to entry in nil map") {
		t.Errorf("нет сообщения о панике: %+v", res)
	}
}

func TestTestBuildFailure(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Test(context.Background(), Files{
		"task.go":      pkgSrc,
		"task_test.go": "package task\n\nimport \"testing\"\n\nfunc TestX(t *testing.T) { Missing() }\n",
	}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if !res.BuildFailed || !strings.Contains(res.BuildOutput, "undefined: Missing") {
		t.Fatalf("Test = %+v", res)
	}
}

func TestTestRaceDetector(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Test(context.Background(), Files{
		"task.go": pkgSrc,
		"task_test.go": `package task

import (
	"sync"
	"testing"
)

func TestRace(t *testing.T) {
	n := 0
	var wg sync.WaitGroup
	for range 2 {
		wg.Go(func() { n++ })
	}
	wg.Wait()
}
`,
	}, Flags{Race: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed {
		t.Fatalf("сборка с -race упала: %s", res.BuildOutput)
	}
	if res.Passed || len(res.Tests) != 1 || !strings.Contains(res.Tests[0].Output+res.Output, "DATA RACE") {
		t.Fatalf("гонка не обнаружена: %+v", res)
	}
}

// Сторонние модули из deps.mod доступны программе без своего go.mod.
func TestDepsAvailable(t *testing.T) {
	e := newExecutor(t)
	src := "package main\n\nimport (\n\t\"fmt\"\n\n\t\"golang.org/x/sync/errgroup\"\n)\n\nfunc main() {\n\tvar g errgroup.Group\n\tg.Go(func() error { return nil })\n\tfmt.Println(g.Wait())\n}\n"
	res, err := e.Run(context.Background(), Files{"main.go": src}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Stdout != "<nil>\n" {
		t.Fatalf("res = %+v", res)
	}
}

// SQLite на чистом Go из deps.mod работает без cgo.
func TestDepsSQLite(t *testing.T) {
	e := newExecutor(t)
	src := "package main\n\nimport (\n\t\"database/sql\"\n\t\"fmt\"\n\n\t_ \"modernc.org/sqlite\"\n)\n\nfunc main() {\n\tdb, _ := sql.Open(\"sqlite\", \":memory:\")\n\tvar n int\n\tfmt.Println(db.QueryRow(\"select 1 + 1\").Scan(&n), n)\n}\n"
	res, err := e.Run(context.Background(), Files{"main.go": src}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Stdout != "<nil> 2\n" {
		t.Fatalf("res = %+v", res)
	}
}

const revSrc = `package main

// Reverse переворачивает строку побайтово — и ломает многобайтовые руны.
func Reverse(s string) string {
	b := []byte(s)
	for i, j := 0, len(b)-1; i < j; i, j = i+1, j-1 {
		b[i], b[j] = b[j], b[i]
	}
	return string(b)
}

func main() {}
`

const revTest = `package main

import (
	"testing"
	"unicode/utf8"
)

func TestReverse(t *testing.T) {
	if got := Reverse("abc"); got != "cba" {
		t.Errorf("Reverse = %q", got)
	}
}

func BenchmarkReverse(b *testing.B) {
	for b.Loop() {
		Reverse("hello")
	}
}

func FuzzReverse(f *testing.F) {
	f.Add("abc")
	f.Fuzz(func(t *testing.T, s string) {
		if utf8.ValidString(s) && !utf8.ValidString(Reverse(s)) {
			t.Errorf("Reverse(%q) — невалидный UTF-8", s)
		}
	})
}
`

func TestGoTestModes(t *testing.T) {
	e := newExecutor(t)
	files := Files{"main.go": revSrc, "main_test.go": revTest}
	ctx := context.Background()

	res, err := e.GoTest(ctx, files, Flags{}, TestOptions{Mode: ModeTest})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.ExitCode != 0 || !strings.Contains(res.Stdout, "--- PASS: TestReverse") || strings.Contains(res.Stdout, "Benchmark") {
		t.Fatalf("test: %+v", res)
	}

	res, err = e.GoTest(ctx, files, Flags{}, TestOptions{Mode: ModeBench})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "BenchmarkReverse") || !strings.Contains(res.Stdout, "allocs/op") || strings.Contains(res.Stdout, "TestReverse") {
		t.Fatalf("bench: %+v", res)
	}

	res, err = e.GoTest(ctx, files, Flags{}, TestOptions{Mode: ModeCover})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode != 0 || !strings.Contains(res.Stdout, "coverage:") || !strings.Contains(res.Coverage, "main.go:4:") || strings.Contains(res.Coverage, "lesson/") {
		t.Fatalf("cover: %+v", res)
	}

	res, err = e.GoTest(ctx, files, Flags{}, TestOptions{Mode: ModeFuzz, Fuzz: "FuzzReverse", FuzzTime: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.ExitCode == 0 || len(res.FuzzInputs) != 1 || !strings.HasPrefix(res.FuzzInputs[0].Name, "testdata/fuzz/FuzzReverse/") ||
		!strings.Contains(res.FuzzInputs[0].Data, "go test fuzz v1") {
		t.Fatalf("fuzz: %+v", res)
	}
}

func TestGoTestRejectsBadOptions(t *testing.T) {
	e := newExecutor(t)
	files := Files{"main.go": revSrc, "main_test.go": revTest}
	for _, opts := range []TestOptions{{Mode: "vet"}, {Mode: ModeFuzz, Fuzz: "FuzzX$ -test.run=."}, {Mode: ModeFuzz}} {
		_, err := e.GoTest(context.Background(), files, Flags{}, opts)
		var in *InputError
		if !errors.As(err, &in) {
			t.Errorf("%+v: err = %v, want InputError", opts, err)
		}
	}
	res, err := e.GoTest(context.Background(), Files{"main.go": revSrc}, Flags{}, TestOptions{Mode: ModeTest})
	if err != nil || !res.BuildFailed || !strings.Contains(res.BuildOutput, "нет тестов") {
		t.Fatalf("без тестов: %+v, %v", res, err)
	}
}
