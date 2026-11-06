package task

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"простые пары", "host = localhost\nport=8080\n", map[string]string{"host": "localhost", "port": "8080"}},
		{"комментарии и пустые строки", "# сервер\n\nhost = a\n   \n  # отступ\n", map[string]string{"host": "a"}},
		{"знак = в значении", "dsn = user=admin password=x\n", map[string]string{"dsn": "user=admin password=x"}},
		{"пустое значение", "token =\n", map[string]string{"token": ""}},
		{"повтор ключа", "a = 1\na = 2\n", map[string]string{"a": "2"}},
		{"без перевода строки в конце", "a = 1", map[string]string{"a": "1"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(strings.NewReader(tt.in))
			if err != nil {
				t.Fatalf("Parse вернул ошибку %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("Parse = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestParseBadLine(t *testing.T) {
	got, err := Parse(strings.NewReader("a = 1\n# комментарий\nbroken\n"))
	if err == nil {
		t.Fatalf("Parse = %v, want ошибку на строке без =", got)
	}
	if !strings.HasPrefix(err.Error(), "line 3: ") {
		t.Errorf("текст ошибки %q должен начинаться с %q", err, "line 3: ")
	}
	if got != nil {
		t.Errorf("при ошибке карта должна быть nil, получено %v", got)
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.conf")
	if err := os.WriteFile(path, []byte("name = demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load вернул ошибку %v", err)
	}
	if got["name"] != "demo" {
		t.Errorf("Load = %v, want name=demo", got)
	}
}
