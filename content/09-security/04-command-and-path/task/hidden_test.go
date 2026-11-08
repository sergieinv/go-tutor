package task

import (
	"archive/zip"
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// exists сообщает, есть ли что-то по пути (симлинк не разыменовывается).
func exists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

// leaked ищет в каталоге dir файл, содержащий секрет.
func leaked(t *testing.T, dir, secret string) string {
	t.Helper()
	found := ""
	filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || d.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		if b, err := os.ReadFile(p); err == nil && strings.Contains(string(b), secret) {
			found = p
		}
		return nil
	})
	return found
}

func TestReadBadNames(t *testing.T) {
	base, _, s := setup(t)
	for _, name := range []string{"", "../secret.txt", "docs/../../secret.txt", filepath.Join(base, "secret.txt")} {
		got, err := s.Read(name)
		if !errors.Is(err, ErrBadName) || got != nil {
			t.Errorf("Read(%q) = %q, %v; want nil, ErrBadName", name, got, err)
		}
	}
}

func TestReadSymlinkInside(t *testing.T) {
	_, dir, s := setup(t)
	// Симлинк, который остаётся внутри каталога, — обычный файл.
	if err := os.Symlink("hello.txt", filepath.Join(dir, "alias.txt")); err != nil {
		t.Fatal(err)
	}
	got, err := s.Read("alias.txt")
	if err != nil || string(got) != "привет" {
		t.Fatalf("Read(alias.txt) = %q, %v; want %q", got, err, "привет")
	}
}

func TestExtractValidatesFirst(t *testing.T) {
	_, dir, s := setup(t)
	zr := zipOf(t, "ok.txt", "ok", "../bad.txt", "bad")
	if err := s.Extract(zr); !errors.Is(err, ErrBadName) {
		t.Fatalf("Extract с ../bad.txt: %v, want ErrBadName", err)
	}
	if exists(filepath.Join(dir, "ok.txt")) {
		t.Errorf("ok.txt записан, хотя архив отвергнут: проверяйте все записи до распаковки")
	}
	for _, bad := range []string{"/abs.txt", "a/../../x.txt", ""} {
		if err := s.Extract(zipOf(t, bad, "x")); !errors.Is(err, ErrBadName) {
			t.Errorf("Extract(%q): %v, want ErrBadName", bad, err)
		}
	}
}

func TestExtractOverwrites(t *testing.T) {
	_, dir, s := setup(t)
	if err := s.Extract(zipOf(t, "hello.txt", "новое")); err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if got := readFile(t, filepath.Join(dir, "hello.txt")); got != "новое" {
		t.Errorf("hello.txt = %q, want %q", got, "новое")
	}
	// Пустой архив — не ошибка.
	if err := s.Extract(zipOf(t)); err != nil {
		t.Errorf("Extract пустого архива: %v", err)
	}
}

// symlinkZip — архив с записью-симлинком link → target и файлом через него.
func symlinkZip(t *testing.T, target string) *zip.Reader {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "link"}
	h.SetMode(fs.ModeSymlink | 0o777)
	w, _ := zw.CreateHeader(h)
	w.Write([]byte(target))
	w, _ = zw.Create("link/evil.txt")
	w.Write([]byte("EVIL"))
	zw.Close()
	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatal(err)
	}
	return zr
}

func TestExtractRejectsSymlinkEntries(t *testing.T) {
	_, dir, s := setup(t)
	if err := s.Extract(symlinkZip(t, "docs")); !errors.Is(err, ErrBadName) {
		t.Fatalf("Extract с записью-симлинком: %v, want ErrBadName", err)
	}
	if exists(filepath.Join(dir, "link")) {
		t.Errorf("link создан, хотя архив отвергнут")
	}
}

func TestThumbnailNames(t *testing.T) {
	_, dir, s := setup(t)
	write(t, filepath.Join(dir, "-dash.png"), "DASH")
	write(t, filepath.Join(dir, "img", "dog.png"), "DOG")
	tests := []struct{ name, out, want string }{
		{"-dash.png", "thumb_-dash.png", "64px:DASH"}, // имя с дефисом — тоже имя файла
		{"img/dog.png", "thumb_dog.png", "64px:DOG"},
	}
	for _, tt := range tests {
		if err := runThumb(t, s, tt.name); err != nil {
			t.Fatalf("утилита для %q: %v", tt.name, err)
		}
		if got := readFile(t, filepath.Join(dir, tt.out)); got != tt.want {
			t.Errorf("%s = %q, want %q", tt.out, got, tt.want)
		}
	}
	for _, bad := range []string{"", "../secret.txt", "/etc/hosts"} {
		cmd, err := s.ThumbnailCmd(tool(t), bad)
		if !errors.Is(err, ErrBadName) || cmd != nil {
			t.Errorf("ThumbnailCmd(%q) = %v, %v; want nil, ErrBadName", bad, cmd, err)
		}
	}
}

func TestExploitReadTraversal(t *testing.T) {
	base, _, s := setup(t)
	if got, err := s.Read("hello.txt"); err != nil || string(got) != "привет" {
		t.Fatalf("обычное чтение не работает: %q, %v", got, err)
	}
	for _, name := range []string{"../secret.txt", "docs/../../secret.txt", filepath.Join(base, "secret.txt"), "../files/../secret.txt"} {
		if got, _ := s.Read(name); strings.Contains(string(got), "SECRET") {
			t.Errorf("Read(%q) прочитал файл вне каталога", name)
		}
	}
}

func TestExploitReadSymlink(t *testing.T) {
	base, dir, s := setup(t)
	if _, err := s.Read("hello.txt"); err != nil {
		t.Fatalf("обычное чтение не работает: %v", err)
	}
	// В каталоге оказался симлинк наружу — например, из чужого архива.
	if err := os.Symlink("../secret.txt", filepath.Join(dir, "avatar.png")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(base, filepath.Join(dir, "up")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"avatar.png", "up/secret.txt"} {
		if got, _ := s.Read(name); strings.Contains(string(got), "SECRET") {
			t.Errorf("Read(%q) прочитал файл вне каталога через симлинк", name)
		}
	}
}

func TestExploitExtractSlip(t *testing.T) {
	base, dir, s := setup(t)
	if err := s.Extract(zipOf(t, "ok.txt", "ok")); err != nil || !exists(filepath.Join(dir, "ok.txt")) {
		t.Fatalf("обычная распаковка не работает: %v", err)
	}
	for _, name := range []string{"../evil.txt", "docs/../../evil.txt", filepath.Join(base, "evil.txt")} {
		s.Extract(zipOf(t, name, "EVIL"))
		if exists(filepath.Join(base, "evil.txt")) {
			t.Fatalf("запись %q распакована вне каталога", name)
		}
	}
}

func TestExploitExtractThroughSymlink(t *testing.T) {
	base, dir, s := setup(t)
	if err := s.Extract(zipOf(t, "ok.txt", "ok")); err != nil {
		t.Fatalf("обычная распаковка не работает: %v", err)
	}
	outside := filepath.Join(base, "outside")
	if err := os.Mkdir(outside, 0o755); err != nil {
		t.Fatal(err)
	}
	// Симлинк на внешний каталог уже лежит в хранилище.
	if err := os.Symlink("../outside", filepath.Join(dir, "out")); err != nil {
		t.Fatal(err)
	}
	s.Extract(zipOf(t, "out/evil.txt", "EVIL"))
	if exists(filepath.Join(outside, "evil.txt")) {
		t.Fatalf("распаковка записала файл вне каталога через симлинк out")
	}
	// Симлинк из самого архива: link → .., затем link/evil.txt.
	s.Extract(symlinkZip(t, ".."))
	if exists(filepath.Join(base, "evil.txt")) {
		t.Fatalf("распаковка записала файл вне каталога через симлинк из архива")
	}
}

func TestExploitThumbnailShell(t *testing.T) {
	_, dir, s := setup(t)
	if err := runThumb(t, s, "cat.png"); err != nil {
		t.Fatalf("утилита для cat.png не работает: %v", err)
	}
	for _, name := range []string{"x.png; touch pwned", "x.png' ; touch pwned; echo '", "$(touch pwned).png", "`touch pwned`.png"} {
		runThumb(t, s, name)
		if exists(filepath.Join(dir, "pwned")) {
			t.Fatalf("имя %q выполнило команду оболочки", name)
		}
	}
}

func TestExploitThumbnailOption(t *testing.T) {
	base, _, s := setup(t)
	if err := runThumb(t, s, "cat.png"); err != nil {
		t.Fatalf("утилита для cat.png не работает: %v", err)
	}
	runThumb(t, s, "-o=../pwned.png")
	if exists(filepath.Join(base, "pwned.png")) {
		t.Fatalf("имя -o=../pwned.png утилита приняла за флаг и записала файл вне каталога")
	}
}

func TestExploitThumbnailTraversal(t *testing.T) {
	_, dir, s := setup(t)
	if err := runThumb(t, s, "cat.png"); err != nil {
		t.Fatalf("утилита для cat.png не работает: %v", err)
	}
	runThumb(t, s, "../secret.txt")
	if p := leaked(t, dir, "SECRET"); p != "" {
		t.Fatalf("утилита прочитала файл вне каталога: секрет в %s", p)
	}
}
