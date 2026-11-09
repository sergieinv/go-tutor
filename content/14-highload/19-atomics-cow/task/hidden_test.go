package task

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
)

func TestHiddenCloneDeep(t *testing.T) {
	src := &Config{Version: 7, Limits: map[string]int{"a": 1}}
	c := src.Clone()
	if c == nil || c == src {
		t.Fatalf("Clone() = %p, src = %p", c, src)
	}
	c.Limits["a"] = 2
	c.Limits["b"] = 3
	if src.Limits["a"] != 1 || len(src.Limits) != 1 || c.Version != 7 {
		t.Fatalf("Clone не глубокая: src = %+v, clone = %+v", src, c)
	}
	empty := (&Config{Version: 2}).Clone()
	if empty == nil || empty.Limits == nil || empty.Version != 2 {
		t.Fatalf("Clone конфигурации без map = %+v, want непустую map и Version 2", empty)
	}
}

func TestHiddenNewHolderCopies(t *testing.T) {
	m := map[string]int{"a": 1}
	h := NewHolder(&Config{Version: 5, Limits: m})
	m["a"] = 99
	m["b"] = 2
	c := mustLoad(t, h)
	if c.Version != 5 || c.Limits["a"] != 1 || len(c.Limits) != 1 {
		t.Fatalf("Load() = %+v после изменения исходной map", c)
	}
	n := mustLoad(t, NewHolder(nil))
	if n.Version != 0 || len(n.Limits) != 0 {
		t.Fatalf("NewHolder(nil).Load() = %+v", n)
	}
	if s := NewHolder(nil).Set("x", 1); s == nil || s.Limits["x"] != 1 || s.Version != 1 {
		t.Fatalf("Set на пустой конфигурации = %+v", s)
	}
}

func TestHiddenNilAndSame(t *testing.T) {
	h := NewHolder(&Config{Limits: map[string]int{"a": 1}})
	before := mustLoad(t, h)
	c, err := h.Update(func(*Config) (*Config, error) { return nil, nil })
	if c != nil || !errors.Is(err, ErrNilConfig) {
		t.Fatalf("Update с nil = %v, %v; want nil, ErrNilConfig", c, err)
	}
	c, err = h.Update(func(old *Config) (*Config, error) { return old, nil })
	if err != nil || c != before {
		t.Fatalf("Update без изменений = %v, %v; want текущий снимок, nil", c, err)
	}
	if after := mustLoad(t, h); after != before || after.Version != 0 {
		t.Fatalf("после пустых обновлений Load() = %+v", after)
	}
}

func TestHiddenVersionSetByHolder(t *testing.T) {
	h := NewHolder(&Config{Version: 10})
	c, err := h.Update(func(old *Config) (*Config, error) {
		n := old.Clone()
		n.Version = 100 // Holder всё равно поставит old.Version + 1
		return n, nil
	})
	if err != nil || c == nil || c.Version != 11 {
		t.Fatalf("Update = %+v, %v; want Version 11", c, err)
	}
}

// Обновления из многих горутин не теряются: f вызывается
// со свежим снимком, пока CAS не пройдёт.
func TestHiddenNoLostUpdates(t *testing.T) {
	h := NewHolder(&Config{Limits: map[string]int{"n": 0}})
	var calls atomic.Int64
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 200 {
				_, err := h.Update(func(old *Config) (*Config, error) {
					calls.Add(1)
					if old == nil {
						return nil, errors.New("old == nil")
					}
					n := old.Clone()
					n.Limits["n"]++
					return n, nil
				})
				if err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	wg.Wait()
	c := mustLoad(t, h)
	if c.Limits["n"] != 1600 || c.Version != 1600 {
		t.Fatalf("n = %d, Version = %d; want 1600, 1600", c.Limits["n"], c.Version)
	}
	if calls.Load() < 1600 {
		t.Fatalf("f вызвана %d раз, want >= 1600", calls.Load())
	}
}

// Читатель никогда не видит частично обновлённого снимка:
// писатели меняют два лимита сразу, и они всегда равны между
// собой и не больше версии; версия, которую видит читатель, не
// убывает. Изменение опубликованной map на месте поймает и -race.
func TestHiddenConsistentSnapshots(t *testing.T) {
	h := NewHolder(&Config{Limits: map[string]int{"a": 0, "b": 0}})
	var done atomic.Bool
	var readers, writers sync.WaitGroup
	var torn atomic.Int64
	for range 4 {
		readers.Go(func() {
			last := -1
			for !done.Load() {
				c := h.Load()
				if c == nil {
					torn.Add(1)
					return
				}
				a, b := c.Limits["a"], c.Limits["b"]
				if a != b || a > c.Version || c.Version < last {
					torn.Add(1)
				}
				last = c.Version
			}
		})
	}
	for range 4 {
		writers.Go(func() {
			for range 150 {
				h.Update(func(old *Config) (*Config, error) {
					n := old.Clone()
					n.Limits["a"] = old.Version + 1
					n.Limits["b"] = old.Version + 1
					return n, nil
				})
				h.Set("other", 1)
				h.Update(func(old *Config) (*Config, error) {
					n := old.Clone()
					n.Limits["a"] = old.Version + 1
					n.Limits["b"] = old.Version + 1
					return n, nil
				})
			}
		})
	}
	writers.Wait()
	done.Store(true)
	readers.Wait()
	if torn.Load() != 0 {
		t.Fatalf("несогласованных снимков: %d", torn.Load())
	}
}
