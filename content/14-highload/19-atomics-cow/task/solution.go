package task

import (
	"errors"
	"maps"
	"sync/atomic"
)

// ErrNilConfig — функция обновления вернула nil без ошибки.
var ErrNilConfig = errors.New("update returned nil config")

// Config — снимок конфигурации. Опубликованный снимок не изменяется.
type Config struct {
	Version int
	Limits  map[string]int
}

// Clone возвращает глубокую копию: новая структура и новая map
// (не nil, даже если у исходной map нет).
func (c *Config) Clone() *Config {
	limits := maps.Clone(c.Limits)
	if limits == nil {
		limits = make(map[string]int)
	}
	return &Config{Version: c.Version, Limits: limits}
}

// Holder хранит текущий снимок конфигурации.
type Holder struct {
	cur atomic.Pointer[Config]
}

// NewHolder создаёт Holder с копией initial: вызывающий может и
// дальше менять свой объект, опубликованный снимок это не заденет.
func NewHolder(initial *Config) *Holder {
	if initial == nil {
		initial = &Config{}
	}
	h := &Holder{}
	h.cur.Store(initial.Clone())
	return h
}

// Load возвращает текущий снимок — один атомарный Load, без блокировок.
func (h *Holder) Load() *Config { return h.cur.Load() }

// Update — CAS-цикл: построить новый снимок по текущему и
// опубликовать, только если текущий за это время не сменился.
// Иначе f вызывается заново со свежим снимком — чужое
// обновление не теряется.
func (h *Holder) Update(f func(old *Config) (*Config, error)) (*Config, error) {
	for {
		old := h.cur.Load()
		next, err := f(old)
		if err != nil {
			return nil, err
		}
		if next == nil {
			return nil, ErrNilConfig
		}
		if next == old {
			return old, nil // изменений нет — нечего публиковать
		}
		// next ещё никому не виден, его можно дописать.
		next.Version = old.Version + 1
		if h.cur.CompareAndSwap(old, next) {
			return next, nil
		}
	}
}

// Set публикует новый снимок, в котором Limits[key] = value.
func (h *Holder) Set(key string, value int) *Config {
	c, _ := h.Update(func(old *Config) (*Config, error) {
		next := old.Clone()
		next.Limits[key] = value
		return next, nil
	})
	return c
}
