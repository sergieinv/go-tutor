package task

import (
	"errors"
	"strings"
)

// ErrSyntax — неверная escape-последовательность.
var ErrSyntax = errors.New("field: неверная escape-последовательность")

// Реализация «в лоб»: на простых строках работает,
// но свойства из условия нарушает. Найдите и исправьте ошибки.

// Escape экранирует обратную косую черту, запятую и перевод строки.
func Escape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, ",", `\,`)
	return strings.ReplaceAll(s, "\n", `\n`)
}

// Unescape — обратная к Escape операция.
func Unescape(s string) (string, error) {
	s = strings.ReplaceAll(s, `\n`, "\n")
	s = strings.ReplaceAll(s, `\,`, ",")
	return strings.ReplaceAll(s, `\\`, `\`), nil
}

// Join экранирует поля и склеивает их через запятую.
func Join(fields []string) string {
	escaped := make([]string, len(fields))
	for i, f := range fields {
		escaped[i] = Escape(f)
	}
	return strings.Join(escaped, ",")
}

// Split делит строку по неэкранированным запятым и раскодирует поля.
func Split(line string) ([]string, error) {
	parts := strings.Split(line, ",")
	for i, p := range parts {
		f, err := Unescape(p)
		if err != nil {
			return nil, err
		}
		parts[i] = f
	}
	return parts, nil
}
