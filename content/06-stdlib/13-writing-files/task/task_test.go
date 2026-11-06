package task

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"testing"
)

func TestWriteAtomicCreates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	err := WriteAtomic(path, func(w io.Writer) error {
		_, err := fmt.Fprint(w, "hello")
		return err
	})
	if err != nil {
		t.Fatalf("WriteAtomic вернул %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("файл не создан: %v", err)
	}
	if string(got) != "hello" {
		t.Errorf("содержимое = %q, want %q", got, "hello")
	}
}

func TestWriteAtomicReplaces(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"v":1, "old": true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteAtomic(path, func(w io.Writer) error {
		_, err := io.WriteString(w, `{"v":2}`)
		return err
	})
	if err != nil {
		t.Fatalf("WriteAtomic вернул %v", err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != `{"v":2}` {
		t.Errorf("содержимое = %q, want %q", got, `{"v":2}`)
	}
}

func TestWriteAtomicPerm(t *testing.T) {
	path := filepath.Join(t.TempDir(), "perm.txt")
	if err := WriteAtomic(path, func(w io.Writer) error { return nil }); err != nil {
		t.Fatalf("WriteAtomic вернул %v", err)
	}
	st, err := os.Stat(path)
	if err != nil {
		t.Fatalf("файл не создан: %v", err)
	}
	if perm := st.Mode().Perm(); perm != 0o644 {
		t.Errorf("права = %o, want 644", perm)
	}
}
