// Run-length encoding: подряд идущие одинаковые байты
// записываются как «число + байт»: `aaab` → `3a1b`.
// В коде есть ошибка, которую таблица тестов не видит,
// а фаззер находит за доли секунды.

package rle

import (
	"errors"
	"strconv"
	"strings"
)

// `Encode` считает длину каждой серии одинаковых байтов.
func Encode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j] == s[i] {
			j++
		}
		b.WriteString(strconv.Itoa(j - i))
		b.WriteByte(s[i])
		i = j
	}
	return b.String()
}

var ErrFormat = errors.New("rle: неверный формат")

// `Decode` читает число (все цифры подряд), затем байт
// и повторяет его. Подвох: что, если сам байт — цифра?
func Decode(s string) (string, error) {
	var b strings.Builder
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && s[j] >= '0' && s[j] <= '9' {
			j++
		}
		if j == i || j == len(s) {
			return "", ErrFormat
		}
		n, err := strconv.Atoi(s[i:j])
		if err != nil {
			return "", ErrFormat
		}
		b.WriteString(strings.Repeat(s[j:j+1], n))
		i = j + 1
	}
	return b.String(), nil
}
