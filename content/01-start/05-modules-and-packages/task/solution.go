package task

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// IsExported сообщает, экспортируется ли идентификатор name:
// его первый символ — заглавная буква Unicode.
func IsExported(name string) bool {
	// Первый символ может занимать несколько байтов
	// (например, «Ё»), поэтому декодируем руну, а не name[0].
	r, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(r)
}

// CanImport сообщает, может ли пакет importer импортировать
// пакет imported с учётом правила каталога internal.
func CanImport(importer, imported string) bool {
	// Ищем последний элемент internal: в середине пути
	// или в самом конце.
	var parent string
	if i := strings.LastIndex(imported, "/internal/"); i >= 0 {
		parent = imported[:i]
	} else if strings.HasSuffix(imported, "/internal") {
		parent = strings.TrimSuffix(imported, "/internal")
	} else {
		return true
	}
	// Дерево, а не строковый префикс: shop2 не лежит внутри shop.
	return importer == parent || strings.HasPrefix(importer, parent+"/")
}
