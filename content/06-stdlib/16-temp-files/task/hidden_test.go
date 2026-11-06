package task

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"
)

func TestWithTempDirFnError(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	boom := errors.New("boom")
	var seen string
	err := WithTempDir("job-", func(dir string) error {
		seen = dir
		os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755)
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WithTempDir вернул %v, want ошибку fn", err)
	}
	if _, err := os.Stat(seen); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("каталог не удалён после ошибки fn")
	}
}

func TestWithTempDirPanic(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var seen string
	func() {
		defer func() {
			if r := recover(); r != "oops" {
				t.Errorf("recover() = %v, want паника из fn должна лететь дальше", r)
			}
		}()
		WithTempDir("p-", func(dir string) error {
			seen = dir
			panic("oops")
		})
	}()
	if seen == "" {
		t.Fatal("fn не была вызвана")
	}
	if _, err := os.Stat(seen); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("каталог не удалён после паники в fn")
	}
}

func TestWithTempDirBadPrefix(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	called := false
	err := WithTempDir("a/b", func(string) error {
		called = true
		return nil
	})
	if err == nil || called {
		t.Errorf("WithTempDir(\"a/b\") = %v, fn вызвана = %v; want ошибку и невызванную fn", err, called)
	}
}

func TestWithTempDirUnique(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	var first string
	WithTempDir("u-", func(a string) error {
		first = a
		return WithTempDir("u-", func(b string) error {
			if a == b {
				t.Errorf("вложенный вызов получил тот же каталог %q", a)
			}
			return nil
		})
	})
	if first == "" {
		t.Error("fn не была вызвана")
	}
}

func TestSpoolReadError(t *testing.T) {
	dir := t.TempDir()
	boom := errors.New("обрыв соединения")
	r := io.MultiReader(strings.NewReader(strings.Repeat("x", 10_000)), iotest.ErrReader(boom))
	path, err := Spool(r, dir)
	if !errors.Is(err, boom) || path != "" {
		t.Fatalf("Spool = %q, %v; want \"\" и ошибку чтения", path, err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("временный файл не удалён: %d файлов в каталоге", len(entries))
	}
}

func TestSpoolBadDir(t *testing.T) {
	path, err := Spool(strings.NewReader("x"), filepath.Join(t.TempDir(), "missing"))
	if err == nil || path != "" {
		t.Errorf("Spool в несуществующий каталог = %q, %v; want \"\" и ошибку", path, err)
	}
}

func TestSpoolEmptyAndUnique(t *testing.T) {
	dir := t.TempDir()
	a, errA := Spool(strings.NewReader(""), dir)
	b, errB := Spool(strings.NewReader(""), dir)
	if errA != nil || errB != nil {
		t.Fatalf("Spool вернул %v, %v", errA, errB)
	}
	if a == b {
		t.Errorf("два вызова вернули один путь %q", a)
	}
	if st, err := os.Stat(a); err != nil || st.Size() != 0 {
		t.Errorf("пустой ввод: файл %q должен существовать и быть пустым (%v)", a, err)
	}
}
