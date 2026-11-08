package task

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrBadName — недопустимое имя файла или записи архива.
var ErrBadName = errors.New("bad file name")

// Store — каталог с файлами пользователей.
type Store struct {
	root *os.Root
}

// NewStore открывает хранилище в каталоге dir.
func NewStore(dir string) (*Store, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	return &Store{root: root}, nil
}

// Close освобождает ресурсы хранилища.
func (s *Store) Close() error {
	return s.root.Close()
}

// checkName отсекает пустые, абсолютные и выходящие наверх имена.
// Симлинки проверкой строки не поймать — их не пропустит os.Root.
func checkName(name string) error {
	if !filepath.IsLocal(name) {
		return fmt.Errorf("%w: %q", ErrBadName, name)
	}
	return nil
}

// Read возвращает содержимое файла name из хранилища.
func (s *Store) Read(name string) ([]byte, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	return s.root.ReadFile(name)
}

// Extract распаковывает архив в хранилище.
func (s *Store) Extract(zr *zip.Reader) error {
	// Сначала проверяем весь архив: при плохой записи не пишем ничего.
	for _, f := range zr.File {
		name := filepath.FromSlash(strings.TrimSuffix(f.Name, "/"))
		if err := checkName(name); err != nil {
			return err
		}
		if m := f.Mode(); !m.IsRegular() && !m.IsDir() {
			return fmt.Errorf("%w: %q не файл и не каталог", ErrBadName, f.Name)
		}
	}
	for _, f := range zr.File {
		name := filepath.FromSlash(strings.TrimSuffix(f.Name, "/"))
		if f.Mode().IsDir() {
			if err := s.root.MkdirAll(name, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := s.root.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			return err
		}
		if err := s.extractFile(f, name); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) extractFile(f *zip.File, name string) error {
	src, err := f.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	dst, err := s.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		return err
	}
	return dst.Close()
}

// ThumbnailCmd готовит запуск утилиты tool для файла name.
func (s *Store) ThumbnailCmd(tool, name string) (*exec.Cmd, error) {
	if err := checkName(name); err != nil {
		return nil, err
	}
	// Без оболочки; "--" — дальше только имена, даже "-o=…".
	cmd := exec.Command(tool, "-size", "64", "--", name)
	cmd.Dir = s.root.Name()
	return cmd, nil
}
