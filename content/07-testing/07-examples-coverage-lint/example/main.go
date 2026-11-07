// Пакет с деньгами в копейках. Тесты к нему — это
// Example-функции: они одновременно документация
// (попадают в `go doc` и на pkg.go.dev) и проверка
// вывода. Комментарии над объявлениями — doc-комментарии:
// они начинаются с имени и пишутся обычным текстом.

package money

import "fmt"

// Amount — сумма в копейках: целые числа не теряют
// копейки при сложении, в отличие от float64.
type Amount int64

// String печатает сумму как 12.34. Знак выводится
// отдельно: -5 / 100 равно 0, и минус бы потерялся.
func (a Amount) String() string {
	sign := ""
	if a < 0 {
		sign = "-"
		a = -a
	}
	return fmt.Sprintf("%s%d.%02d", sign, a/100, a%100)
}

// Split делит неотрицательную сумму на n почти
// равных долей: лишние копейки достаются первым долям,
// сумма долей равна исходной. При n <= 0 возвращает nil.
func (a Amount) Split(n int) []Amount {
	if n <= 0 {
		return nil
	}
	parts := make([]Amount, n)
	base, rest := a/Amount(n), a%Amount(n)
	for i := range parts {
		parts[i] = base
		if Amount(i) < rest {
			parts[i]++
		}
	}
	return parts
}

// Totals суммирует траты по категориям.
func Totals(spent map[string][]Amount) map[string]Amount {
	res := make(map[string]Amount, len(spent))
	for cat, list := range spent {
		for _, a := range list {
			res[cat] += a
		}
	}
	return res
}
