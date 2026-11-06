package task

import "cmp"

// Number — числовые типы, включая именованные на их основе.
type Number interface {
	~int | ~int64 | ~float64
}

// Sum возвращает сумму элементов s.
func Sum[T Number](s []T) T {
	var total T
	for _, v := range s {
		total += v
	}
	return total
}

// Clamp ограничивает v отрезком [lo, hi]. При lo > hi — паника.
func Clamp[T cmp.Ordered](v, lo, hi T) T {
	if lo > hi {
		panic("Clamp: lo > hi")
	}
	return min(max(v, lo), hi)
}

// MaxBy возвращает элемент с наибольшим ключом (первый при равенстве).
// Для пустого s — нулевое значение и false.
func MaxBy[T any, K cmp.Ordered](s []T, key func(T) K) (T, bool) {
	var best T
	if len(s) == 0 {
		return best, false
	}
	best = s[0]
	bestKey := key(best)
	for _, v := range s[1:] {
		// Строгое > оставляет первый из равных.
		if k := key(v); k > bestKey {
			best, bestKey = v, k
		}
	}
	return best, true
}
