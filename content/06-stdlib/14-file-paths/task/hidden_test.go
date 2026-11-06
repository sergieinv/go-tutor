package task

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestSafeJoinEdge(t *testing.T) {
	ok := []struct{ base, name, want string }{
		{"data", ".", "data"},
		{"data", "a//b/", filepath.Join("data", "a", "b")},
		{"/srv/files", "x/./y.txt", filepath.Join("/srv/files", "x", "y.txt")},
		{"data", "..data/x", filepath.Join("data", "..data", "x")},
	}
	for _, tt := range ok {
		got, err := SafeJoin(tt.base, tt.name)
		if err != nil || got != tt.want {
			t.Errorf("SafeJoin(%q, %q) = %q, %v; want %q, nil", tt.base, tt.name, got, err, tt.want)
		}
	}
	bad := []string{"", "..", "a/../../x", "../data2/x", "a/b/../../../data/x", "/"}
	for _, name := range bad {
		got, err := SafeJoin("data", name)
		if !errors.Is(err, ErrUnsafePath) || got != "" {
			t.Errorf("SafeJoin(%q, %q) = %q, %v; want \"\", ErrUnsafePath", "data", name, got, err)
		}
	}
}

func TestReplaceExtEdge(t *testing.T) {
	tests := []struct {
		p, ext, want string
	}{
		{".bashrc", ".bak", ".bashrc.bak"},
		{"home/.profile", "", "home/.profile"},
		{".config.old", ".bak", ".config.bak"},
		{"v1.2/readme", ".md", "v1.2/readme.md"},
		{"./a//b.txt", ".md", "./a//b.md"},
		{"archive.tar.gz", "", "archive.tar"},
		{"a.b.c", ".c", "a.b.c"},
	}
	for _, tt := range tests {
		if got := ReplaceExt(tt.p, tt.ext); got != tt.want {
			t.Errorf("ReplaceExt(%q, %q) = %q, want %q", tt.p, tt.ext, got, tt.want)
		}
	}
}
