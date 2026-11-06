package task

import "sync"

// Counter — потокобезопасный счётчик по ключам.
// Нулевое значение готово к работе.
type Counter struct {
	mu sync.Mutex
	m  map[string]int
}

// Inc увеличивает счётчик key на 1.
func (c *Counter) Inc(key string) {
	// TODO: реализуйте метод
}

// Value возвращает текущее значение счётчика key.
func (c *Counter) Value(key string) int {
	// TODO: реализуйте метод
	return 0
}

// Snapshot возвращает копию всех счётчиков.
func (c *Counter) Snapshot() map[string]int {
	// TODO: реализуйте метод
	return nil
}
