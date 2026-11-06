package task

import (
	"errors"
	"io/fs"
)

// ErrBadLang — недопустимый код языка.
var ErrBadLang = errors.New("bad language code")

// Greeting возвращает приветствие на языке lang с откатом на en.
func Greeting(fsys fs.FS, lang string) (string, error) {
	// TODO: реализуйте функцию
	return "", nil
}

// Languages возвращает отсортированные коды языков из greetings/*.txt.
func Languages(fsys fs.FS) ([]string, error) {
	// TODO: реализуйте функцию
	return nil, nil
}
