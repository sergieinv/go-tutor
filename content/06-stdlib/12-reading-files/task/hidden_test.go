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

func TestParseCRLF(t *testing.T) {
	got, err := Parse(strings.NewReader("a = 1\r\nb = 2\r\n"))
	if err != nil || got["a"] != "1" || got["b"] != "2" || len(got) != 2 {
		t.Errorf("Parse(CRLF) = %q, %v; want a=1, b=2", got, err)
	}
}

func TestParseLongLine(t *testing.T) {
	long := strings.Repeat("x", 1_000_000)
	got, err := Parse(strings.NewReader("a = 1\nblob = " + long + "\nz = 2\n"))
	if err != nil {
		t.Fatalf("Parse со строкой в 1 000 000 байт вернул ошибку %v — поднимите лимит Scanner через Buffer", err)
	}
	if len(got["blob"]) != len(long) || got["z"] != "2" {
		t.Errorf("длинная строка прочитана не полностью: len(blob) = %d, z = %q", len(got["blob"]), got["z"])
	}
}

func TestParseEmptyKey(t *testing.T) {
	_, err := Parse(strings.NewReader("\n\n = value\n"))
	if err == nil || !strings.HasPrefix(err.Error(), "line 3: ") {
		t.Errorf("Parse вернул %v, want ошибку, начинающуюся с %q", err, "line 3: ")
	}
}

func TestParseEmptyInput(t *testing.T) {
	got, err := Parse(strings.NewReader(""))
	if err != nil || got == nil || len(got) != 0 {
		t.Errorf("Parse(\"\") = %#v, %v; want пустую не nil карту и nil", got, err)
	}
}

func TestParseReadError(t *testing.T) {
	boom := errors.New("диск отвалился")
	r := io.MultiReader(strings.NewReader("a = 1\n"), iotest.ErrReader(boom))
	got, err := Parse(r)
	if !errors.Is(err, boom) {
		t.Fatalf("Parse вернул ошибку %v, want обёртку над ошибкой чтения", err)
	}
	if got != nil {
		t.Errorf("при ошибке карта должна быть nil, получено %v", got)
	}
}

func TestLoadNotExist(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.conf"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("Load(нет файла) вернул %v, want ошибку с fs.ErrNotExist", err)
	}
}

func TestLoadBadFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.conf")
	if err := os.WriteFile(path, []byte("ok = 1\noops\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil || !strings.Contains(err.Error(), "line 2: ") {
		t.Errorf("Load вернул %v, want ошибку разбора со строкой 2", err)
	}
}
