package task

import "sync/atomic"

// Gauge собирает статистику наблюдений без мьютекса.
// Нулевое значение готово к работе.
type Gauge struct {
	count atomic.Int64
	// nil — наблюдений не было. Так нулевое значение Gauge
	// отличает «максимум 0» от «максимума нет», и отрицательные
	// наблюдения работают без особого начального значения.
	max atomic.Pointer[int64]
}

// Observe учитывает одно наблюдение v.
func (g *Gauge) Observe(v int64) {
	g.count.Add(1)
	// CAS-цикл: заменить указатель, только если за время
	// сравнения его никто не подменил.
	for {
		cur := g.max.Load()
		if cur != nil && *cur >= v {
			return
		}
		if g.max.CompareAndSwap(cur, &v) {
			return
		}
	}
}

// Count возвращает число наблюдений.
func (g *Gauge) Count() int64 {
	return g.count.Load()
}

// Max возвращает наибольшее наблюдение; false — если
// наблюдений ещё не было.
func (g *Gauge) Max() (int64, bool) {
	p := g.max.Load()
	if p == nil {
		return 0, false
	}
	return *p, true
}
