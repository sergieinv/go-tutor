package livetrace

import (
	"context"
	"testing"
	"time"

	"gotutor/runner/sandbox"
)

const raceReport = `==================
WARNING: DATA RACE
Read at 0x00c00019e048 by goroutine 7:
  main.(*stats).inc()
      /tmp/run-1/main.go:10 +0x80
  main.main.func1()
      /tmp/run-1/main.go:20 +0x7c

Previous write at 0x00c00019e048 by main goroutine:
  runtime.mapaccess2_fast64()
      /usr/local/go/src/internal/runtime/maps/runtime_fast64.go:22 +0x2ac
  main.main()
      /tmp/run-1/main.go:24 +0x90

Goroutine 7 (running) created at:
  main.main()
      /tmp/run-1/main.go:18 +0x88
==================
2
Found 1 data race(s)
`

func TestParseRace(t *testing.T) {
	r := ParseRace(raceReport)
	if len(r) != 1 {
		t.Fatalf("отчётов %d", len(r))
	}
	a := r[0].Accesses
	if len(a) != 2 || a[0].Kind != "read" || a[0].G != 7 || a[1].Kind != "write" || a[1].G != 0 {
		t.Fatalf("accesses = %+v", a)
	}
	if got := a[0].Stack; len(got) != 2 || got[0] != (RaceFrame{"main.(*stats).inc", "main.go", 10}) || got[1].Line != 20 {
		t.Errorf("stack = %+v", got)
	}
	// Кадры рантайма отброшены.
	if got := a[1].Stack; len(got) != 1 || got[0].Line != 24 {
		t.Errorf("stack = %+v", got)
	}
	g := r[0].Goroutines
	if len(g) != 1 || g[0].G != 7 || g[0].State != "running" || g[0].Created[0].Line != 18 {
		t.Errorf("goroutines = %+v", g)
	}
	if len(ParseRace("ok\n")) != 0 {
		t.Error("отчёт из ничего")
	}
}

func TestRunRace(t *testing.T) {
	e := newExecutor(t)
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	src := "package main\n\nimport (\n\t\"fmt\"\n\t\"sync\"\n)\n\nfunc main() {\n\tn := 0\n\tvar wg sync.WaitGroup\n\tfor range 2 {\n\t\twg.Add(1)\n\t\tgo func() {\n\t\t\tdefer wg.Done()\n\t\t\tn++\n\t\t}()\n\t}\n\twg.Wait()\n\tfmt.Println(n > 0)\n}\n"
	res, err := RunRace(ctx, e, sandbox.Files{"main.go": src}, sandbox.Flags{})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Races) == 0 || res.ExitCode != 66 {
		t.Fatalf("гонка не найдена: exit=%d\n%s", res.ExitCode, res.Stderr)
	}
	for _, a := range res.Races[0].Accesses {
		if len(a.Stack) == 0 || a.Stack[0].Line != 15 {
			t.Errorf("доступ %+v: ждали строку 15", a)
		}
	}
}
