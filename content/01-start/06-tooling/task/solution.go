package task

import (
	"fmt"
	"strings"
)

// CountVerbs возвращает, сколько аргументов потребляет
// форматная строка: %% — 0, директива %[флаги][ширина][.точность]буква — 1.
// Для некорректного формата возвращает -1.
func CountVerbs(format string) int {
	count := 0
	// Идём по байтам: все значимые символы формата — ASCII,
	// а байты многобайтовых символов UTF-8 всегда >= 0x80
	// и ни с одним из них не совпадут.
	for i := 0; i < len(format); i++ {
		if format[i] != '%' {
			continue
		}
		i++
		if i < len(format) && format[i] == '%' {
			continue // %% — литеральный знак процента
		}
		for i < len(format) && strings.IndexByte("+-# 0", format[i]) >= 0 {
			i++
		}
		for i < len(format) && isDigit(format[i]) {
			i++
		}
		if i < len(format) && format[i] == '.' {
			i++
			for i < len(format) && isDigit(format[i]) {
				i++
			}
		}
		if i >= len(format) || !isLetter(format[i]) {
			return -1
		}
		count++
	}
	return count
}

func isDigit(c byte) bool  { return '0' <= c && c <= '9' }
func isLetter(c byte) bool { return 'a' <= c && c <= 'z' || 'A' <= c && c <= 'Z' }

// CheckCall проверяет вызов Printf(format, <nargs аргументов>):
// "" — всё в порядке, "bad format" — формат некорректен,
// "format reads N args, but call has M" — число аргументов не совпадает.
func CheckCall(format string, nargs int) string {
	want := CountVerbs(format)
	switch {
	case want < 0:
		return "bad format"
	case want != nargs:
		return fmt.Sprintf("format reads %d args, but call has %d", want, nargs)
	}
	return ""
}
