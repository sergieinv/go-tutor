package task

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ErrUnsafePath — путь пустой, абсолютный или выходит за пределы базового каталога.
var ErrUnsafePath = errors.New("unsafe path")

// SafeJoin присоединяет к base относительный путь name в форме со слешами.
func SafeJoin(base, name string) (string, error) {
	local := filepath.FromSlash(name)
	// IsLocal: не пустой, не абсолютный, после Clean не начинается с "..".
	if !filepath.IsLocal(local) {
		return "", fmt.Errorf("%w: %q", ErrUnsafePath, name)
	}
	return filepath.Join(base, local), nil
}

// ReplaceExt заменяет расширение последнего элемента пути p на ext.
func ReplaceExt(p, ext string) string {
	old := filepath.Ext(p)
	// ".bashrc": точка в начале имени — не расширение.
	if old == filepath.Base(p) {
		old = ""
	}
	return strings.TrimSuffix(p, old) + ext
}
