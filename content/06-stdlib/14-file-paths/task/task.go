package task

import "errors"

// ErrUnsafePath — путь пустой, абсолютный или выходит за пределы базового каталога.
var ErrUnsafePath = errors.New("unsafe path")

// SafeJoin присоединяет к base относительный путь name в форме со слешами.
func SafeJoin(base, name string) (string, error) {
	// TODO: реализуйте функцию
	return "", nil
}

// ReplaceExt заменяет расширение последнего элемента пути p на ext.
func ReplaceExt(p, ext string) string {
	// TODO: реализуйте функцию
	return ""
}
