package task

import "io"

// WithTempDir создаёт временный каталог, вызывает fn и удаляет каталог.
func WithTempDir(prefix string, fn func(dir string) error) error {
	// TODO: реализуйте функцию
	return nil
}

// Spool копирует r во временный файл в каталоге dir и возвращает его путь.
func Spool(r io.Reader, dir string) (string, error) {
	// TODO: реализуйте функцию
	return "", nil
}
