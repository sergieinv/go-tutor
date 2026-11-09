package task

import (
	"errors"
	"sync/atomic"
)

// ErrNilConfig — функция обновления вернула nil без ошибки.
var ErrNilConfig = errors.New("update returned nil config")

// Config — снимок конфигурации. Опубликованный снимок не изменяется.
type Config struct {
	Version int
	Limits  map[string]int
}

// Clone возвращает глубокую копию: новая структура и новая map.
func (c *Config) Clone() *Config {
	// TODO
	return nil
}

// Holder хранит текущий снимок конфигурации.
type Holder struct {
	cur atomic.Pointer[Config]
}

// NewHolder создаёт Holder с копией initial (nil — пустая конфигурация).
func NewHolder(initial *Config) *Holder {
	// TODO
	return &Holder{}
}

// Load возвращает текущий снимок.
func (h *Holder) Load() *Config {
	// TODO
	return nil
}

// Update публикует снимок, построенный функцией f по текущему.
func (h *Holder) Update(f func(old *Config) (*Config, error)) (*Config, error) {
	// TODO
	return nil, nil
}

// Set публикует новый снимок, в котором Limits[key] = value.
func (h *Holder) Set(key string, value int) *Config {
	// TODO
	return nil
}
