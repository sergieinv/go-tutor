package task

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// fakeTB записывает вызовы методов testing.TB по порядку.
type fakeTB struct {
	testing.TB
	calls    []string // "Helper", "Errorf: …", "Fatalf: …", "Cleanup"
	cleanups []func()
}

func (f *fakeTB) Helper() { f.calls = append(f.calls, "Helper") }
func (f *fakeTB) Errorf(format string, args ...any) {
	f.calls = append(f.calls, "Errorf: "+fmt.Sprintf(format, args...))
}
func (f *fakeTB) Error(args ...any) { f.calls = append(f.calls, "Error: "+fmt.Sprint(args...)) }
func (f *fakeTB) Fatalf(format string, args ...any) {
	f.calls = append(f.calls, "Fatalf: "+fmt.Sprintf(format, args...))
	runtime.Goexit() // как у настоящего T: остановить горутину теста
}
func (f *fakeTB) Fatal(args ...any) {
	f.calls = append(f.calls, "Fatal: "+fmt.Sprint(args...))
	runtime.Goexit()
}
func (f *fakeTB) Cleanup(fn func()) {
	f.calls = append(f.calls, "Cleanup")
	f.cleanups = append(f.cleanups, fn)
}

// runCleanups выполняет зарегистрированные функции, как testing: в обратном порядке.
func (f *fakeTB) runCleanups() {
	for i := len(f.cleanups) - 1; i >= 0; i-- {
		f.cleanups[i]()
	}
	f.cleanups = nil
}

// run вызывает хелпер в отдельной горутине — Fatalf завершает именно её.
func run(t *testing.T, fn func(tb testing.TB)) *fakeTB {
	t.Helper()
	f := &fakeTB{TB: t}
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn(f)
	}()
	<-done
	return f
}

func wantCalls(t *testing.T, f *fakeTB, want ...string) {
	t.Helper()
	if fmt.Sprintf("%q", f.calls) != fmt.Sprintf("%q", want) {
		t.Errorf("вызовы tb:\n got %q\nwant %q", f.calls, want)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestGoldenMatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ok.golden")
	writeFile(t, path, "итого: 650\n")
	f := run(t, func(tb testing.TB) { Golden(tb, path, "итого: 650\n", false) })
	wantCalls(t, f, "Helper")
}

func TestGoldenDiff(t *testing.T) {
	path := filepath.Join(t.TempDir(), "diff.golden")
	writeFile(t, path, "a\nc\n")
	f := run(t, func(tb testing.TB) { Golden(tb, path, "a\nb", false) })
	wantCalls(t, f, "Helper", fmt.Sprintf("Errorf: %s: строка 2:\n got: \"b\"\nwant: \"c\"", path))
}

func TestGoldenUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new.golden")
	f := run(t, func(tb testing.TB) { Golden(tb, path, "свежий эталон\n", true) })
	wantCalls(t, f, "Helper")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "свежий эталон\n" {
		t.Errorf("после update файл = %q, %v; want %q", b, err, "свежий эталон\n")
	}
}

func TestSwap(t *testing.T) {
	mode := "prod"
	f := run(t, func(tb testing.TB) { Swap(tb, &mode, "test") })
	wantCalls(t, f, "Helper", "Cleanup")
	if mode != "test" {
		t.Fatalf("после Swap mode = %q, want %q", mode, "test")
	}
	f.runCleanups()
	if mode != "prod" {
		t.Errorf("после Cleanup mode = %q, want %q", mode, "prod")
	}
}
