package task

// Counter возвращает функцию, которая возвращает start,
// start+step, start+2*step... при последовательных вызовах.
func Counter(start, step int) func() int {
	// next — захваченная переменная: у каждого
	// вызова Counter своя.
	next := start
	return func() int {
		v := next
		next += step
		return v
	}
}

// Memo оборачивает f кэшем. call(n) вызывает f(n) только
// при первом запросе n. calls() — сколько раз вызвана f.
func Memo(f func(int) int) (call func(int) int, calls func() int) {
	// Обе функции захватывают одни и те же cache и count.
	cache := make(map[int]int)
	count := 0
	call = func(n int) int {
		if v, ok := cache[n]; ok {
			return v
		}
		count++
		v := f(n)
		cache[n] = v
		return v
	}
	calls = func() int { return count }
	return call, calls
}
