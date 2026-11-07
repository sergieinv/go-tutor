// Тестируем две небольшие функции обработки текста:
// у каждой много входов и ожидаемых выходов — идеальный
// случай для табличного теста.

package text

import (
	"strings"
	"unicode"
)

// Slug делает из заголовка часть URL: буквы и цифры
// в нижнем регистре, всё остальное — дефисы, без повторов
// и без дефисов по краям.
func Slug(title string) string {
	var b strings.Builder
	dash := false
	for _, r := range title {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if dash && b.Len() > 0 {
				b.WriteByte('-')
			}
			b.WriteRune(unicode.ToLower(r))
			dash = false
		} else {
			dash = true
		}
	}
	return b.String()
}

// Freq считает слова без учёта регистра.
func Freq(s string) map[string]int {
	m := map[string]int{}
	for _, w := range strings.Fields(strings.ToLower(s)) {
		m[w]++
	}
	return m
}

// Words возвращает слова строки в нижнем регистре. Срез
// растёт через `append`, поэтому для строки без слов — nil.
func Words(s string) []string {
	var out []string
	for _, w := range strings.Fields(s) {
		out = append(out, strings.ToLower(w))
	}
	return out
}
