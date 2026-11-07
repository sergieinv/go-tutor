package task

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// Golden сравнивает got с golden-файлом path или перезаписывает его при update.
func Golden(tb testing.TB, path, got string, update bool) {
	tb.Helper()
	if update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			tb.Fatalf("golden: %v", err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			tb.Fatalf("golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		tb.Fatalf("golden: %v", err)
	}
	if got == string(want) {
		return
	}
	gl, wl := strings.Split(got, "\n"), strings.Split(string(want), "\n")
	for i := 0; ; i++ {
		g, w := line(gl, i), line(wl, i)
		if g != w {
			tb.Errorf("%s: строка %d:\n got: %s\nwant: %s", path, i+1, g, w)
			return
		}
	}
}

// line возвращает i-ю строку в кавычках или <EOF>, если строк меньше.
func line(lines []string, i int) string {
	if i >= len(lines) {
		return "<EOF>"
	}
	return strconv.Quote(lines[i])
}

// Swap присваивает *p = v до конца теста и восстанавливает старое значение.
func Swap[T any](tb testing.TB, p *T, v T) {
	tb.Helper()
	old := *p
	*p = v
	tb.Cleanup(func() { *p = old })
}
