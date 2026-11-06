package task

// Counter возвращает функцию, которая возвращает start,
// start+step, start+2*step... при последовательных вызовах.
func Counter(start, step int) func() int {
	// TODO: реализуйте функцию
	return func() int { return 0 }
}

// Memo оборачивает f кэшем. call(n) вызывает f(n) только
// при первом запросе n. calls() — сколько раз вызвана f.
func Memo(f func(int) int) (call func(int) int, calls func() int) {
	// TODO: реализуйте функцию
	return f, func() int { return 0 }
}
