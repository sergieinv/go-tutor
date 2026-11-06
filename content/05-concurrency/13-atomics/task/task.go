package task

// Gauge собирает статистику наблюдений без мьютекса.
// Нулевое значение готово к работе.
type Gauge struct {
	// TODO: поля
}

// Observe учитывает одно наблюдение v.
func (g *Gauge) Observe(v int64) {
	// TODO: реализуйте метод
}

// Count возвращает число наблюдений.
func (g *Gauge) Count() int64 {
	// TODO: реализуйте метод
	return 0
}

// Max возвращает наибольшее наблюдение; false — если
// наблюдений ещё не было.
func (g *Gauge) Max() (int64, bool) {
	// TODO: реализуйте метод
	return 0, false
}
