package task

import (
	"maps"
	"sync"
)

// Counter — потокобезопасный счётчик по ключам.
// Нулевое значение готово к работе.
type Counter struct {
	mu sync.Mutex
	m  map[string]int
}

// Inc увеличивает счётчик key на 1.
func (c *Counter) Inc(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m == nil {
		// Ленивая инициализация — тоже под мьютексом:
		// иначе две горутины создадут две разные map.
		c.m = make(map[string]int)
	}
	c.m[key]++
}

// Value возвращает текущее значение счётчика key.
func (c *Counter) Value(key string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.m[key] // чтение из nil map безопасно и даёт 0
}

// Snapshot возвращает копию всех счётчиков.
func (c *Counter) Snapshot() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Отдать c.m наружу нельзя: вызывающий читал бы её
	// без мьютекса, пока другие горутины пишут.
	out := make(map[string]int, len(c.m))
	maps.Copy(out, c.m)
	return out
}
