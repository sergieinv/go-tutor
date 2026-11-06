package task

import "sync"

// Cache загружает значение каждого ключа ровно один раз.
type Cache[K comparable, V any] struct {
	load func(K) (V, error)

	mu      sync.Mutex
	entries map[K]func() (V, error)
}

// NewCache создаёт кеш с функцией загрузки load.
func NewCache[K comparable, V any](load func(K) (V, error)) *Cache[K, V] {
	return &Cache[K, V]{load: load, entries: map[K]func() (V, error){}}
}

// Get возвращает результат load(key), вызывая load не больше раза на ключ.
func (c *Cache[K, V]) Get(key K) (V, error) {
	// Мьютекс защищает только карту и держится мгновенно.
	c.mu.Lock()
	get, ok := c.entries[key]
	if !ok {
		// OnceValues запоминает и значение, и ошибку,
		// а панику load повторяет при каждом вызове.
		get = sync.OnceValues(func() (V, error) { return c.load(key) })
		c.entries[key] = get
	}
	c.mu.Unlock()

	// Загрузка — уже без мьютекса: другие ключи не ждут.
	return get()
}
