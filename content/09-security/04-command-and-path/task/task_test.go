package task

import (
	"archive/zip"
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// Тестовый бинарник умеет притворяться внешней утилитой миниатюр:
// thumb [-size N] [-o выход] [вход]. Без входного файла читает stdin,
// результат по умолчанию пишет в thumb_<имя входа> в текущем каталоге.
func TestMain(m *testing.M) {
	if os.Getenv("THUMB_TOOL") == "1" {
		os.Exit(thumbTool(os.Args[1:]))
	}
	os.Exit(m.Run())
}

func thumbTool(args []string) int {
	fset := flag.NewFlagSet("thumb", flag.ContinueOnError)
	size := fset.Int("size", 128, "размер")
	out := fset.String("o", "", "куда писать")
	if fset.Parse(args) != nil || fset.NArg() > 1 {
		return 2
	}
	in := "stdin"
	var data []byte
	var err error
	if fset.NArg() == 1 {
		in = fset.Arg(0)
		data, err = os.ReadFile(in)
	} else {
		data, err = io.ReadAll(os.Stdin)
	}
	if err != nil {
		return 1
	}
	if *out == "" {
		*out = "thumb_" + filepath.Base(in)
	}
	if os.WriteFile(*out, fmt.Appendf(nil, "%dpx:%s", *size, data), 0o644) != nil {
		return 1
	}
	return 0
}

// tool — путь к утилите для ThumbnailCmd.
func tool(t *testing.T) string {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	return exe
}

// runThumb запускает команду в режиме утилиты; ошибку утилиты возвращает.
func runThumb(t *testing.T, s *Store, name string) error {
	t.Helper()
	cmd, err := s.ThumbnailCmd(tool(t), name)
	if err != nil {
		return err
	}
	if cmd == nil {
		t.Fatalf("ThumbnailCmd(%q) = nil, nil", name)
	}
	cmd.Env = append(os.Environ(), "THUMB_TOOL=1")
	return cmd.Run()
}

// setup создаёт base/files с несколькими файлами и base/secret.txt
// рядом. Возвращает base, files и открытое хранилище.
func setup(t *testing.T) (base, dir string, s *Store) {
	t.Helper()
	base = t.TempDir()
	dir = filepath.Join(base, "files")
	write(t, filepath.Join(dir, "hello.txt"), "привет")
	write(t, filepath.Join(dir, "docs", "a.txt"), "документ")
	write(t, filepath.Join(dir, "cat.png"), "CAT")
	write(t, filepath.Join(base, "secret.txt"), "SECRET")
	s, err := NewStore(dir)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	if s == nil {
		t.Fatalf("NewStore вернул nil без ошибки")
	}
	t.Cleanup(func() { s.Close() })
	return base, dir, s
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("прочитать %s: %v", path, err)
	}
	return string(b)
}

// zipOf собирает архив из пар «имя записи — содержимое».
func zipOf(t *testing.T, entries ...string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i+1 < len(entries); i += 2 {
		w, err := zw.Create(entries[i])
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(entries[i+1]))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestRead(t *testing.T) {
	_, _, s := setup(t)
	for name, want := range map[string]string{"hello.txt": "привет", "docs/a.txt": "документ"} {
		got, err := s.Read(name)
		if err != nil || string(got) != want {
			t.Errorf("Read(%q) = %q, %v; want %q", name, got, err, want)
		}
	}
	if _, err := s.Read("nope.txt"); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Read(nope.txt): ошибка %v, want fs.ErrNotExist", err)
	}
}

func TestExtract(t *testing.T) {
	_, dir, s := setup(t)
	zr := zipOf(t, "report.txt", "отчёт", "img/", "", "img/logo.png", "LOGO", "a/b/c.txt", "глубоко")
	if err := s.Extract(zr); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	for name, want := range map[string]string{"report.txt": "отчёт", "img/logo.png": "LOGO", "a/b/c.txt": "глубоко"} {
		if got := readFile(t, filepath.Join(dir, name)); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
}

func TestThumbnail(t *testing.T) {
	_, dir, s := setup(t)
	if err := runThumb(t, s, "cat.png"); err != nil {
		t.Fatalf("утилита для cat.png: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "thumb_cat.png")); got != "64px:CAT" {
		t.Errorf("thumb_cat.png = %q, want %q", got, "64px:CAT")
	}
}
