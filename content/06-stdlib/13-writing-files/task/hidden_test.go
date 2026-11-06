package task

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// names возвращает имена файлов каталога.
func names(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestWriteAtomicManySmallWrites(t *testing.T) {
	path := filepath.Join(t.TempDir(), "big.txt")
	var want strings.Builder
	err := WriteAtomic(path, func(w io.Writer) error {
		for i := range 2000 {
			s := fmt.Sprintf("line %d\n", i)
			want.WriteString(s)
			if _, err := io.WriteString(w, s); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WriteAtomic вернул %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != want.String() {
		t.Errorf("записано %d байт, want %d — не забыт ли Flush?", len(got), want.Len())
	}
}

func TestWriteAtomicCallbackErrorKeepsOld(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "data.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	boom := errors.New("генерация упала")
	err := WriteAtomic(path, func(w io.Writer) error {
		io.WriteString(w, strings.Repeat("new", 5000))
		return boom
	})
	if !errors.Is(err, boom) {
		t.Fatalf("WriteAtomic вернул %v, want ошибку write", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != "old" {
		t.Errorf("прежний файл изменён: %q", got)
	}
	if n := names(t, dir); !slices.Equal(n, []string{"data.txt"}) {
		t.Errorf("в каталоге остались лишние файлы: %v", n)
	}
}

func TestWriteAtomicNoLeftovers(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "a.txt")
	for i := range 3 {
		err := WriteAtomic(path, func(w io.Writer) error {
			_, err := fmt.Fprint(w, i)
			return err
		})
		if err != nil {
			t.Fatalf("WriteAtomic вернул %v", err)
		}
	}
	if n := names(t, dir); !slices.Equal(n, []string{"a.txt"}) {
		t.Errorf("в каталоге = %v, want только a.txt", n)
	}
	if got, _ := os.ReadFile(path); string(got) != "2" {
		t.Errorf("содержимое = %q, want %q", got, "2")
	}
}

func TestWriteAtomicMissingDir(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nope", "a.txt")
	called := false
	err := WriteAtomic(path, func(w io.Writer) error {
		called = true
		return nil
	})
	if err == nil {
		t.Fatal("WriteAtomic в несуществующий каталог вернул nil")
	}
	if called {
		t.Error("write не должен вызываться, если временный файл не создан")
	}
	if n := names(t, dir); len(n) != 0 {
		t.Errorf("созданы лишние файлы: %v", n)
	}
}

func TestWriteAtomicRenameError(t *testing.T) {
	dir := t.TempDir()
	// На месте целевого файла — непустой каталог: Rename не сможет его заменить.
	path := filepath.Join(dir, "target")
	if err := os.MkdirAll(filepath.Join(path, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	err := WriteAtomic(path, func(w io.Writer) error {
		_, err := io.WriteString(w, "x")
		return err
	})
	if err == nil {
		t.Fatal("WriteAtomic вернул nil, хотя Rename поверх каталога невозможен")
	}
	if n := names(t, dir); !slices.Equal(n, []string{"target"}) {
		t.Errorf("временный файл не удалён после ошибки Rename: %v", n)
	}
}
