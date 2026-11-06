package task

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWithTempDir(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp) // os.TempDir() в Unix читает $TMPDIR

	var seen string
	err := WithTempDir("work-", func(dir string) error {
		seen = dir
		if filepath.Dir(dir) != tmp {
			t.Errorf("каталог %q создан не в os.TempDir()", dir)
		}
		if !strings.HasPrefix(filepath.Base(dir), "work-") {
			t.Errorf("имя каталога %q не начинается с work-", filepath.Base(dir))
		}
		return os.WriteFile(filepath.Join(dir, "a.txt"), []byte("x"), 0o644)
	})
	if err != nil {
		t.Fatalf("WithTempDir вернул %v", err)
	}
	if seen == "" {
		t.Fatal("fn не была вызвана")
	}
	if _, err := os.Stat(seen); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("каталог %q не удалён после WithTempDir", seen)
	}
}

func TestSpool(t *testing.T) {
	dir := t.TempDir()
	path, err := Spool(strings.NewReader("hello, spool"), dir)
	if err != nil {
		t.Fatalf("Spool вернул %v", err)
	}
	if filepath.Dir(path) != dir {
		t.Errorf("файл %q создан не в %q", path, dir)
	}
	base := filepath.Base(path)
	if !strings.HasPrefix(base, "spool-") || !strings.HasSuffix(base, ".tmp") {
		t.Errorf("имя %q не соответствует шаблону spool-*.tmp", base)
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "hello, spool" {
		t.Errorf("содержимое = %q, %v; want %q", got, err, "hello, spool")
	}
}
