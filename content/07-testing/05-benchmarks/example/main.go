// Бенчмарк сравнивает реализации на одних и тех же
// данных. Здесь две функции склейки строк: наивная
// через `+=` и через `strings.Builder` с заранее
// выделенной памятью. Результат у них одинаковый,
// цена — разная.

package main

import "strings"

// `+=` создаёт новую строку на каждой итерации и копирует
// в неё всё накопленное: O(n²) байт копирования и
// по аллокации на каждую часть.
func JoinNaive(parts []string, sep string) string {
	s := ""
	for i, p := range parts {
		if i > 0 {
			s += sep
		}
		s += p
	}
	return s
}

// `Builder` дописывает в один растущий буфер, а `Grow`
// заранее выделяет ровно нужный объём — одна аллокация
// на весь результат. `String()` не копирует буфер.
func JoinBuilder(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	n := len(sep) * (len(parts) - 1)
	for _, p := range parts {
		n += len(p)
	}
	var b strings.Builder
	b.Grow(n)
	for i, p := range parts {
		if i > 0 {
			b.WriteString(sep)
		}
		b.WriteString(p)
	}
	return b.String()
}

// В режиме тестов `main` не нужна, но пакет `main`
// без неё не соберётся.
func main() {}
