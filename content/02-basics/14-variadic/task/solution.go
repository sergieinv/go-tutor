package task

// Sum возвращает сумму аргументов; без аргументов — 0.
func Sum(nums ...int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

// Clamp возвращает новый срез, где каждое число ограничено
// отрезком [lo, hi]. Переданные данные не меняются.
// Без аргументов — пустой срез (не nil). При lo > hi — panic.
func Clamp(lo, hi int, nums ...int) []int {
	if lo > hi {
		panic("Clamp: lo > hi")
	}
	// При вызове Clamp(lo, hi, s...) nums — это сам s,
	// поэтому пишем в новый срез, а не в nums.
	out := make([]int, len(nums))
	for i, n := range nums {
		out[i] = min(max(n, lo), hi)
	}
	return out
}
