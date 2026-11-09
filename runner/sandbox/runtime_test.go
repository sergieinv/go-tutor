package sandbox

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
)

// Флаги модуля «Механизмы рантайма»: -gcflags, GOTRACEBACK и pprof.

const escapeProgram = `package main

import "fmt"

type point struct{ x, y int }

func newPoint() *point { return &point{1, 2} }

func main() {
	p := newPoint()
	fmt.Println(p.x)
}
`

func TestGCFlagsEscape(t *testing.T) {
	e := newExecutor(t)
	res, err := e.Run(context.Background(), Files{"main.go": escapeProgram}, Flags{GCFlags: "-m"})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.Stdout != "1\n" {
		t.Fatalf("Run = %+v", res)
	}
	if !strings.Contains(res.CompilerOutput, "./main.go:7:33: &point{...} escapes to heap") {
		t.Errorf("нет диагностики escape analysis:\n%s", res.CompilerOutput)
	}
	if strings.Contains(res.CompilerOutput, "# ") {
		t.Errorf("строка «# пакет» не убрана:\n%s", res.CompilerOutput)
	}

	// Без флагов диагностики нет.
	res, err = e.Run(context.Background(), Files{"main.go": escapeProgram}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.CompilerOutput != "" {
		t.Errorf("CompilerOutput без GCFlags: %q", res.CompilerOutput)
	}
}

func TestGCFlagsBoundsChecks(t *testing.T) {
	src := "package main\n\nimport \"os\"\n\nfunc main() {\n\ts := make([]int, len(os.Args))\n\ti := len(os.Args) - 1\n\tprintln(s[i])\n}\n"
	res, err := newExecutor(t).Run(context.Background(), Files{"main.go": src}, Flags{GCFlags: "-d=ssa/check_bce/debug=1"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.CompilerOutput, "./main.go:8:11: Found IsInBounds") {
		t.Errorf("нет проверки границ:\n%s", res.CompilerOutput)
	}
}

func TestValidGCFlags(t *testing.T) {
	for _, ok := range []string{"", "-m", "-m -m", "-m=2 -l", "-N -l", "-d=ssa/check_bce/debug=1"} {
		if err := ValidGCFlags(ok); err != nil {
			t.Errorf("ValidGCFlags(%q) = %v", ok, err)
		}
	}
	// Флаги, которые пишут файлы или подключают чужой код, — нет.
	for _, bad := range []string{"-S", "all=-m", "-m -o=/tmp/x", "-asmhdr=/etc/x", "-importcfg=x", "-m;-l"} {
		if ValidGCFlags(bad) == nil {
			t.Errorf("ValidGCFlags(%q) = nil", bad)
		}
	}
	if ValidTraceback("all") != nil || ValidTraceback("crash") == nil {
		t.Error("ValidTraceback")
	}
}

func TestTraceback(t *testing.T) {
	src := "package main\n\nimport \"time\"\n\nfunc main() {\n\tgo func() { time.Sleep(time.Hour) }()\n\ttime.Sleep(10 * time.Millisecond)\n\tpanic(\"бум\")\n}\n"
	e := newExecutor(t)
	for _, tc := range []struct {
		level      string
		goroutines bool
	}{{"", false}, {"all", true}} {
		res, err := e.Run(context.Background(), Files{"main.go": src}, Flags{GOTRACEBACK: tc.level})
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Contains(res.Stderr, "time.Sleep"); got != tc.goroutines || res.ExitCode != 2 {
			t.Errorf("GOTRACEBACK=%q: стек второй горутины %v, exit %d\n%s", tc.level, got, res.ExitCode, res.Stderr)
		}
	}
}

func pprofExecutor(t *testing.T) *Executor {
	t.Helper()
	bin := os.Getenv("RUNNER_PPROF")
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("pprof"); err != nil {
			t.Skip("нет pprof: go build -o <путь>/pprof cmd/pprof и RUNNER_PPROF")
		}
	}
	e := newExecutor(t)
	e.cfg.Pprof = bin
	return e
}

func TestProfile(t *testing.T) {
	src := `package main

import (
	"os"
	"runtime/pprof"
)

var sink [][]byte

func main() {
	for range 1000 {
		sink = append(sink, make([]byte, 4096))
	}
	f, _ := os.Create("heap.pprof")
	pprof.Lookup("allocs").WriteTo(f, 0)
	f.Close()
	os.WriteFile("broken.pprof", []byte("не профиль"), 0o644)
	os.Symlink("/etc/passwd", "link.pprof")
	println("ok")
}
`
	res, err := pprofExecutor(t).Profile(context.Background(), Files{"main.go": src}, Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if res.BuildFailed || res.ExitCode != 0 {
		t.Fatalf("Profile = %+v", res.RunResult)
	}
	if len(res.Profiles) != 2 {
		t.Fatalf("профили: %+v", res.Profiles)
	}
	broken, heap := res.Profiles[0], res.Profiles[1]
	if broken.Name != "broken.pprof" || !broken.Failed {
		t.Errorf("битый профиль: %+v", broken)
	}
	if heap.Name != "heap.pprof" || heap.Failed || !strings.Contains(heap.Top, "main.main") || !strings.Contains(heap.Top, "flat%") {
		t.Errorf("профиль памяти:\n%s", heap.Top)
	}
}

func TestProfileDisabled(t *testing.T) {
	_, err := newExecutor(t).Profile(context.Background(), Files{"main.go": "package main\n\nfunc main() {}\n"}, Flags{})
	if !errors.Is(err, ErrNoPprof) {
		t.Fatalf("err = %v", err)
	}
}
