package task

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"
)

// file возвращает файл заданного размера.
func file(size int) *fstest.MapFile {
	return &fstest.MapFile{Data: []byte(strings.Repeat("x", size))}
}

func TestFindLarge(t *testing.T) {
	fsys := fstest.MapFS{
		"readme.md":         file(10),
		"video.mp4":         file(500),
		"src/main.go":       file(50),
		"src/assets/bg.png": file(300),
		"node_modules/x.js": file(1000),
	}
	got, err := FindLarge(fsys, 100, []string{"node_modules"})
	if err != nil {
		t.Fatalf("FindLarge вернул ошибку %v", err)
	}
	want := []string{"src/assets/bg.png", "video.mp4"}
	if !slices.Equal(got, want) {
		t.Errorf("FindLarge = %v, want %v", got, want)
	}
}

func TestFindLargeBoundary(t *testing.T) {
	fsys := fstest.MapFS{
		"a.bin": file(99),
		"b.bin": file(100),
	}
	got, err := FindLarge(fsys, 100, nil)
	if err != nil || !slices.Equal(got, []string{"b.bin"}) {
		t.Errorf("FindLarge = %v, %v; want [b.bin] — размер ровно minSize подходит", got, err)
	}
}
