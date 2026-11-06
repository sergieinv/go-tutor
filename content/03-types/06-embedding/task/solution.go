package task

import "errors"

// Store — хранилище строк по ключу.
type Store interface {
	Get(key string) (string, bool)
	Set(key, value string) error
}

// MapStore — простая реализация Store на map.
type MapStore map[string]string

func (m MapStore) Get(key string) (string, bool) {
	v, ok := m[key]
	return v, ok
}

func (m MapStore) Set(key, value string) error {
	m[key] = value
	return nil
}

// ErrReadOnly возвращает Set у ReadOnlyStore.
var ErrReadOnly = errors.New("store is read-only")

// CountingStore пересылает вызовы во встроенный Store и считает их.
type CountingStore struct {
	Store
	Gets, Sets int
}

// Метод с тем же именем затеняет продвинутый Store.Get;
// до встроенного добираемся явно: c.Store.Get.
func (c *CountingStore) Get(key string) (string, bool) {
	c.Gets++
	return c.Store.Get(key)
}

func (c *CountingStore) Set(key, value string) error {
	c.Sets++
	return c.Store.Set(key, value)
}

// ReadOnlyStore пропускает чтение и запрещает запись.
// Get продвигается из встроенного Store — свой метод не нужен.
type ReadOnlyStore struct {
	Store
}

func (r ReadOnlyStore) Set(key, value string) error {
	return ErrReadOnly
}
