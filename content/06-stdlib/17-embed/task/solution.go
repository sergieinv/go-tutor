package task

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"
	"strings"
)

// ErrBadLang — недопустимый код языка.
var ErrBadLang = errors.New("bad language code")

func validLang(lang string) bool {
	if lang == "" {
		return false
	}
	for _, r := range lang {
		if (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

// Greeting возвращает приветствие на языке lang с откатом на en.
func Greeting(fsys fs.FS, lang string) (string, error) {
	if !validLang(lang) {
		return "", fmt.Errorf("%w: %q", ErrBadLang, lang)
	}
	// Пути fs.FS — всегда со слешами: path.Join, а не filepath.Join.
	data, err := fs.ReadFile(fsys, path.Join("greetings", lang+".txt"))
	if errors.Is(err, fs.ErrNotExist) {
		data, err = fs.ReadFile(fsys, "greetings/en.txt")
	}
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(data)), nil
}

// Languages возвращает отсортированные коды языков из greetings/*.txt.
func Languages(fsys fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(fsys, "greetings")
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var langs []string
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".txt")
		// Только коды, которые примет Greeting: ".txt" дал бы пустой код.
		if ok && !e.IsDir() && validLang(name) {
			langs = append(langs, name)
		}
	}
	slices.Sort(langs) // ReadDir уже сортирует, но не полагаемся на реализацию FS
	return langs, nil
}
