package task

// Cache загружает значение каждого ключа ровно один раз.
type Cache[K comparable, V any] struct {
	// TODO: поля
}

// NewCache создаёт кеш с функцией загрузки load.
func NewCache[K comparable, V any](load func(K) (V, error)) *Cache[K, V] {
	// TODO: реализуйте функцию
	return &Cache[K, V]{}
}

// Get возвращает результат load(key), вызывая load не больше раза на ключ.
func (c *Cache[K, V]) Get(key K) (V, error) {
	// TODO
	var zero V
	return zero, nil
}
