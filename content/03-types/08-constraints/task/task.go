package task

import "cmp"

// Number — числовые типы, включая именованные на их основе.
type Number interface {
	~int | ~int64 | ~float64
}

// Sum возвращает сумму элементов s.
func Sum[T Number](s []T) T {
	// TODO: реализуйте функцию
	var zero T
	return zero
}

// Clamp ограничивает v отрезком [lo, hi]. При lo > hi — паника.
func Clamp[T cmp.Ordered](v, lo, hi T) T {
	// TODO: реализуйте функцию
	var zero T
	return zero
}

// MaxBy возвращает элемент с наибольшим ключом (первый при равенстве).
// Для пустого s — нулевое значение и false.
func MaxBy[T any, K cmp.Ordered](s []T, key func(T) K) (T, bool) {
	// TODO: реализуйте функцию
	var zero T
	return zero, false
}
