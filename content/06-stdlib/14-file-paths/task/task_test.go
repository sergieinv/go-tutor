package task

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSafeJoin(t *testing.T) {
	tests := []struct {
		base, name string
		want       string
	}{
		{"data", "a.txt", filepath.Join("data", "a.txt")},
		{"data", "img/logo.png", filepath.Join("data", "img", "logo.png")},
		{"data", "a/../b.txt", filepath.Join("data", "b.txt")},
	}
	for _, tt := range tests {
		got, err := SafeJoin(tt.base, tt.name)
		if err != nil || got != tt.want {
			t.Errorf("SafeJoin(%q, %q) = %q, %v; want %q, nil", tt.base, tt.name, got, err, tt.want)
		}
	}
}

func TestSafeJoinUnsafe(t *testing.T) {
	for _, name := range []string{"../secret", "/etc/passwd"} {
		got, err := SafeJoin("data", name)
		if !errors.Is(err, ErrUnsafePath) || got != "" {
			t.Errorf("SafeJoin(%q, %q) = %q, %v; want \"\", ErrUnsafePath", "data", name, got, err)
		}
	}
}

func TestReplaceExt(t *testing.T) {
	tests := []struct {
		p, ext, want string
	}{
		{"photo.jpeg", ".jpg", "photo.jpg"},
		{"docs/report.tar.gz", ".zip", "docs/report.tar.zip"},
		{"bin/tool", ".exe", "bin/tool.exe"},
		{"notes.txt", "", "notes"},
	}
	for _, tt := range tests {
		if got := ReplaceExt(tt.p, tt.ext); got != tt.want {
			t.Errorf("ReplaceExt(%q, %q) = %q, want %q", tt.p, tt.ext, got, tt.want)
		}
	}
}
