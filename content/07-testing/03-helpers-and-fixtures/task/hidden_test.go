package task

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestHiddenGoldenMissingFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.golden")
	_, readErr := os.ReadFile(path)
	f := run(t, func(tb testing.TB) { Golden(tb, path, "x", false) })
	wantCalls(t, f, "Helper", "Fatalf: golden: "+readErr.Error())
}

func TestHiddenGoldenTrailingNewline(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nl.golden")
	writeFile(t, path, "a")
	f := run(t, func(tb testing.TB) { Golden(tb, path, "a\n", false) })
	wantCalls(t, f, "Helper", fmt.Sprintf("Errorf: %s: строка 2:\n got: \"\"\nwant: <EOF>", path))

	writeFile(t, path, "a\nb\nc")
	f = run(t, func(tb testing.TB) { Golden(tb, path, "a\nb", false) })
	wantCalls(t, f, "Helper", fmt.Sprintf("Errorf: %s: строка 3:\n got: <EOF>\nwant: \"c\"", path))
}

func TestHiddenGoldenFirstLineAndQuoting(t *testing.T) {
	path := filepath.Join(t.TempDir(), "q.golden")
	writeFile(t, path, "tab\there\nsame\nдругое")
	// Отличаются строки 1 и 3 — сообщение только о первой.
	f := run(t, func(tb testing.TB) { Golden(tb, path, "tab  here\nsame\nиное", false) })
	wantCalls(t, f, "Helper", fmt.Sprintf("Errorf: %s: строка 1:\n got: %q\nwant: %q", path, "tab  here", "tab\there"))
}

func TestHiddenGoldenEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.golden")
	writeFile(t, path, "")
	f := run(t, func(tb testing.TB) { Golden(tb, path, "", false) })
	wantCalls(t, f, "Helper")
	f = run(t, func(tb testing.TB) { Golden(tb, path, "x", false) })
	wantCalls(t, f, "Helper", fmt.Sprintf("Errorf: %s: строка 1:\n got: \"x\"\nwant: \"\"", path))
}

func TestHiddenGoldenUpdateCreatesDirs(t *testing.T) {
	path := filepath.Join(t.TempDir(), "testdata", "deep", "r.golden")
	f := run(t, func(tb testing.TB) { Golden(tb, path, "v1", true) })
	wantCalls(t, f, "Helper")
	f = run(t, func(tb testing.TB) { Golden(tb, path, "v2\n", true) }) // перезапись
	wantCalls(t, f, "Helper")
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "v2\n" {
		t.Fatalf("файл = %q, %v; want %q", b, err, "v2\n")
	}
	info, _ := os.Stat(path)
	if perm := info.Mode().Perm(); perm&0o600 != 0o600 {
		t.Errorf("права файла %v, want 0644", perm)
	}
	// После обновления сравнение с тем же значением проходит без сообщений.
	f = run(t, func(tb testing.TB) { Golden(tb, path, "v2\n", false) })
	wantCalls(t, f, "Helper")
}

func TestHiddenGoldenUpdateError(t *testing.T) {
	// Каталог на месте файла: записать не получится.
	path := t.TempDir()
	f := run(t, func(tb testing.TB) { Golden(tb, path, "x", true) })
	if len(f.calls) != 2 || f.calls[0] != "Helper" || !strings.HasPrefix(f.calls[1], "Fatalf: golden: ") {
		t.Errorf("вызовы tb = %q, want [Helper, Fatalf: golden: …]", f.calls)
	}
}

func TestHiddenGoldenHelperFirstOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "h.golden")
	writeFile(t, path, "1")
	f := run(t, func(tb testing.TB) { Golden(tb, path, "2", false) })
	if len(f.calls) == 0 || f.calls[0] != "Helper" {
		t.Errorf("первый вызов tb = %q, want Helper (вызовы: %q)", first(f.calls), f.calls)
	}
}

func first(s []string) string {
	if len(s) == 0 {
		return "<нет вызовов>"
	}
	return s[0]
}

func TestHiddenSwapNested(t *testing.T) {
	limit := 10
	f := run(t, func(tb testing.TB) {
		Swap(tb, &limit, 20)
		Swap(tb, &limit, 30)
	})
	wantCalls(t, f, "Helper", "Cleanup", "Helper", "Cleanup")
	if limit != 30 {
		t.Fatalf("после двух Swap limit = %d, want 30", limit)
	}
	f.runCleanups()
	if limit != 10 {
		t.Errorf("после Cleanup limit = %d, want исходное 10", limit)
	}
}

func TestHiddenSwapFunc(t *testing.T) {
	// Типичное применение — подмена часов.
	now := time.Now
	fixed := time.Date(2025, 8, 12, 0, 0, 0, 0, time.UTC)
	f := run(t, func(tb testing.TB) { Swap(tb, &now, func() time.Time { return fixed }) })
	if got := now(); !got.Equal(fixed) {
		t.Fatalf("now() = %v, want %v", got, fixed)
	}
	f.runCleanups()
	if got := now(); got.Equal(fixed) {
		t.Error("после Cleanup now всё ещё подменена")
	}
}

func TestHiddenSwapNotRestoredEarly(t *testing.T) {
	// Значение должно держаться до Cleanup, а не до выхода из Swap.
	type config struct{ debug bool }
	cfg := config{}
	f := run(t, func(tb testing.TB) { Swap(tb, &cfg, config{debug: true}) })
	if !cfg.debug {
		t.Fatal("значение вернулось сразу после Swap — используйте tb.Cleanup, а не defer")
	}
	if len(f.cleanups) != 1 {
		t.Fatalf("зарегистрировано Cleanup-функций: %d, want 1", len(f.cleanups))
	}
	f.runCleanups()
	if cfg.debug {
		t.Error("после Cleanup значение не восстановлено")
	}
}
