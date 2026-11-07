package task

import (
	"errors"
	"strings"
)

// ErrSyntax — неверная escape-последовательность.
var ErrSyntax = errors.New("field: неверная escape-последовательность")

// Escape экранирует обратную косую черту, запятую и перевод строки.
func Escape(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case ',':
			b.WriteString(`\,`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(c)
		}
	}
	return b.String()
}

// Unescape — обратная к Escape операция.
func Unescape(s string) (string, error) {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		// Читаем последовательность целиком: `\` и следующий байт.
		i++
		if i == len(s) {
			return "", ErrSyntax
		}
		switch s[i] {
		case '\\':
			b.WriteByte('\\')
		case ',':
			b.WriteByte(',')
		case 'n':
			b.WriteByte('\n')
		default:
			return "", ErrSyntax
		}
	}
	return b.String(), nil
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
	var fields []string
	start := 0
	for i := 0; i < len(line); i++ {
		switch line[i] {
		case '\\':
			i++ // следующий байт экранирован, даже если это запятая
		case ',':
			f, err := Unescape(line[start:i])
			if err != nil {
				return nil, err
			}
			fields = append(fields, f)
			start = i + 1
		}
	}
	f, err := Unescape(line[start:])
	if err != nil {
		return nil, err
	}
	return append(fields, f), nil
}
