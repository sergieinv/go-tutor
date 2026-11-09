package task

import (
	"errors"
	"sync"
	"testing"
)

func mustLoad(t *testing.T, h *Holder) *Config {
	t.Helper()
	c := h.Load()
	if c == nil {
		t.Fatal("Load() = nil")
	}
	return c
}

func TestSetAndLoad(t *testing.T) {
	h := NewHolder(&Config{Limits: map[string]int{"api": 10}})
	c := h.Set("db", 5)
	if c == nil || c.Version != 1 || c.Limits["db"] != 5 || c.Limits["api"] != 10 {
		t.Fatalf("Set вернул %+v", c)
	}
	if got := mustLoad(t, h); got != c {
		t.Fatalf("Load() = %+v, want снимок из Set", got)
	}
}

func TestOldSnapshotUnchanged(t *testing.T) {
	h := NewHolder(nil)
	old := mustLoad(t, h)
	h.Set("api", 1)
	if old.Version != 0 || len(old.Limits) != 0 {
		t.Fatalf("старый снимок изменился: %+v", old)
	}
}

func TestUpdateError(t *testing.T) {
	h := NewHolder(nil)
	before := mustLoad(t, h)
	boom := errors.New("boom")
	c, err := h.Update(func(*Config) (*Config, error) { return nil, boom })
	if c != nil || !errors.Is(err, boom) {
		t.Fatalf("Update = %v, %v; want nil, boom", c, err)
	}
	if mustLoad(t, h) != before {
		t.Fatal("после ошибки опубликован новый снимок")
	}
}

func TestConcurrentSet(t *testing.T) {
	h := NewHolder(nil)
	var wg sync.WaitGroup
	for g := range 4 {
		key := string(rune('a' + g))
		wg.Go(func() {
			for i := 1; i <= 50; i++ {
				h.Set(key, i)
			}
		})
	}
	wg.Wait()
	c := mustLoad(t, h)
	if c.Version != 200 {
		t.Fatalf("Version = %d, want 200", c.Version)
	}
	for g := range 4 {
		if v := c.Limits[string(rune('a'+g))]; v != 50 {
			t.Fatalf("Limits[%c] = %d, want 50", 'a'+g, v)
		}
	}
}
