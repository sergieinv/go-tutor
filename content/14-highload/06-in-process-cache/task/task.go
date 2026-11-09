package task

import "time"

// Cache — LRU-кеш с ограничением размера и сроком жизни записей
// (см. «Задание»). Безопасен для одновременного использования.
type Cache[K comparable, V any] struct {
	// TODO: состояние кеша
}

func New[K comparable, V any](capacity int, ttl time.Duration, now func() time.Time) *Cache[K, V] {
	// TODO
	return &Cache[K, V]{}
}

func (c *Cache[K, V]) Get(k K) (V, bool) {
	// TODO
	var zero V
	return zero, false
}

func (c *Cache[K, V]) Set(k K, v V) {
	// TODO
}

func (c *Cache[K, V]) Delete(k K) {
	// TODO
}

func (c *Cache[K, V]) Len() int {
	// TODO
	return 0
}

func (c *Cache[K, V]) Purge() int {
	// TODO
	return 0
}
