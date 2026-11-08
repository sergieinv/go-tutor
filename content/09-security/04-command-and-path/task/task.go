package task

import (
	"archive/zip"
	"errors"
	"os/exec"
)

// ErrBadName — недопустимое имя файла или записи архива.
var ErrBadName = errors.New("bad file name")

var errNotImplemented = errors.New("not implemented")

// Store — каталог с файлами пользователей.
type Store struct {
	// TODO: поля
}

// NewStore открывает хранилище в каталоге dir.
func NewStore(dir string) (*Store, error) {
	// TODO: реализуйте функцию
	return nil, errNotImplemented
}

// Close освобождает ресурсы хранилища.
func (s *Store) Close() error {
	// TODO: реализуйте метод
	return nil
}

// Read возвращает содержимое файла name из хранилища.
func (s *Store) Read(name string) ([]byte, error) {
	// TODO: реализуйте метод
	return nil, errNotImplemented
}

// Extract распаковывает архив в хранилище.
func (s *Store) Extract(zr *zip.Reader) error {
	// TODO: реализуйте метод
	return errNotImplemented
}

// ThumbnailCmd готовит запуск утилиты tool для файла name.
func (s *Store) ThumbnailCmd(tool, name string) (*exec.Cmd, error) {
	// TODO: реализуйте метод
	return nil, errNotImplemented
}
