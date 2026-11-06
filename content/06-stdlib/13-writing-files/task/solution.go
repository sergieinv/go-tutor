package task

import (
	"bufio"
	"io"
	"os"
	"path/filepath"
)

// WriteAtomic записывает файл path целиком или не трогает его вовсе.
func WriteAtomic(path string, write func(w io.Writer) error) (err error) {
	// Временный файл — в том же каталоге: Rename атомарен
	// только в пределах одной файловой системы.
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			tmp.Close() // повторный Close безопасен, ошибку игнорируем
			os.Remove(tmp.Name())
		}
	}()

	w := bufio.NewWriter(tmp)
	if err := write(w); err != nil {
		return err
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmp.Name(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
