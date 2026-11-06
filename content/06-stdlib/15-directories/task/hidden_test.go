package task

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"testing/fstest"
)

func TestFindLargeSkipNested(t *testing.T) {
	fsys := fstest.MapFS{
		"app/vendor/lib/big.a": file(500),
		"app/big.a":            file(500),
		".git/objects/pack":    file(500),
		"docs/.git":            file(500), // файл с именем из skipDirs не пропускается
	}
	got, err := FindLarge(fsys, 1, []string{"vendor", ".git"})
	if err != nil {
		t.Fatalf("FindLarge вернул ошибку %v", err)
	}
	want := []string{"app/big.a", "docs/.git"}
	if !slices.Equal(got, want) {
		t.Errorf("FindLarge = %v, want %v", got, want)
	}
}

func TestFindLargeFileNamedLikeSkipDir(t *testing.T) {
	// Если вернуть SkipDir для файла "cache", WalkDir пропустит его соседей "d.bin" и "e.bin".
	fsys := fstest.MapFS{
		"data/a.bin": file(10),
		"data/cache": file(10),
		"data/d.bin": file(10),
		"data/e.bin": file(10),
	}
	got, err := FindLarge(fsys, 1, []string{"cache"})
	if err != nil {
		t.Fatalf("FindLarge вернул ошибку %v", err)
	}
	want := []string{"data/a.bin", "data/cache", "data/d.bin", "data/e.bin"}
	if !slices.Equal(got, want) {
		t.Errorf("FindLarge = %v, want %v", got, want)
	}
}

func TestFindLargeSorted(t *testing.T) {
	fsys := fstest.MapFS{
		"a/x":   file(1),
		"a-b/y": file(1),
		"a.txt": file(1),
	}
	got, err := FindLarge(fsys, 1, nil)
	want := []string{"a-b/y", "a.txt", "a/x"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("FindLarge = %v, %v; want %v в лексикографическом порядке", got, err, want)
	}
}

func TestFindLargeNothing(t *testing.T) {
	got, err := FindLarge(fstest.MapFS{"a": file(1)}, 10, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("FindLarge = %v, %v; want пустой результат и nil", got, err)
	}
	got, err = FindLarge(fstest.MapFS{}, 0, nil)
	if err != nil || len(got) != 0 {
		t.Errorf("FindLarge(пустая ФС) = %v, %v; want пустой результат и nil", got, err)
	}
}

func TestFindLargeRootNotSkipped(t *testing.T) {
	got, err := FindLarge(fstest.MapFS{"f": file(5)}, 1, []string{"."})
	if err != nil || !slices.Equal(got, []string{"f"}) {
		t.Errorf("FindLarge = %v, %v; want [f] — корень не пропускается", got, err)
	}
}

func TestFindLargeOnDisk(t *testing.T) {
	dir := t.TempDir()
	for name, size := range map[string]int{"logs/app.log": 2048, "logs/tmp/x.log": 4096, "small.txt": 3} {
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := FindLarge(os.DirFS(dir), 1024, []string{"tmp"})
	if err != nil || !slices.Equal(got, []string{"logs/app.log"}) {
		t.Errorf("FindLarge(os.DirFS) = %v, %v; want [logs/app.log]", got, err)
	}
}

// brokenFS не может открыть каталог "bad".
// Только метод Open: иначе fs.ReadDir возьмёт ReadDir у MapFS в обход ошибки.
type brokenFS struct {
	m   fstest.MapFS
	err error
}

func (b brokenFS) Open(name string) (fs.File, error) {
	if name == "bad" {
		return nil, &fs.PathError{Op: "open", Path: name, Err: b.err}
	}
	return b.m.Open(name)
}

func TestFindLargeWalkError(t *testing.T) {
	denied := errors.New("доступ запрещён")
	fsys := brokenFS{m: fstest.MapFS{"a.bin": file(10), "bad/x.bin": file(10)}, err: denied}
	got, err := FindLarge(fsys, 1, nil)
	if !errors.Is(err, denied) {
		t.Fatalf("FindLarge вернул ошибку %v, want ошибку обхода", err)
	}
	if got != nil {
		t.Errorf("при ошибке результат должен быть nil, получено %v", got)
	}
}
