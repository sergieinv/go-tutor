package task

import "slices"

// Without возвращает новый срез без элемента с индексом i.
// Если i вне диапазона, возвращает копию s. s не меняется.
// Результат никогда не nil.
func Without(s []int, i int) []int {
	if i < 0 || i >= len(s) {
		// slices.Clone(nil) вернул бы nil, а нужен пустой срез.
		return append([]int{}, s...)
	}
	// Новый массив нужной длины: append(s[:i], ...) испортил бы s.
	out := make([]int, 0, len(s)-1)
	out = append(out, s[:i]...)
	return append(out, s[i+1:]...)
}

// TopN возвращает n наибольших элементов s по убыванию.
// При n > len(s) — все элементы, при n <= 0 — пустой срез.
// s не меняется. Результат никогда не nil.
func TopN(s []int, n int) []int {
	if n <= 0 {
		return []int{}
	}
	// Sort работает на месте, поэтому сортируем копию (не nil даже для пустого s).
	sorted := append([]int{}, s...)
	slices.Sort(sorted)
	slices.Reverse(sorted)
	return sorted[:min(n, len(sorted))]
}
