package task

import (
	"io"
	"os"
)

// WithTempDir создаёт временный каталог, вызывает fn и удаляет каталог.
func WithTempDir(prefix string, fn func(dir string) error) (err error) {
	dir, err := os.MkdirTemp("", prefix+"*")
	if err != nil {
		return err
	}
	// defer срабатывает и при панике в fn.
	defer func() {
		if rerr := os.RemoveAll(dir); rerr != nil && err == nil {
			err = rerr
		}
	}()
	return fn(dir)
}

// Spool копирует r во временный файл в каталоге dir и возвращает его путь.
func Spool(r io.Reader, dir string) (path string, err error) {
	f, err := os.CreateTemp(dir, "spool-*.tmp")
	if err != nil {
		return "", err
	}
	defer func() {
		if err != nil {
			f.Close()
			os.Remove(f.Name())
			path = ""
		}
	}()
	if _, err := io.Copy(f, r); err != nil {
		return "", err
	}
	if err := f.Close(); err != nil {
		return "", err
	}
	return f.Name(), nil
}
