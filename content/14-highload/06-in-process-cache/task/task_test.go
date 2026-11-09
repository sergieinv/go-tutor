package task

import (
	"sync"
	"testing"
	"time"
)

type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 3, 1, 9, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.t }
func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func mustGet[K comparable, V comparable](t *testing.T, c *Cache[K, V], k K, want V) {
	t.Helper()
	got, ok := c.Get(k)
	if !ok || got != want {
		t.Fatalf("Get(%v) = %v, %v; want %v, true", k, got, ok, want)
	}
}

func mustMiss[K comparable, V any](t *testing.T, c *Cache[K, V], k K) {
	t.Helper()
	if got, ok := c.Get(k); ok {
		t.Fatalf("Get(%v) = %v, true; want промах", k, got)
	}
}

func TestLRUEviction(t *testing.T) {
	clk := newClock()
	c := New[string, int](2, time.Hour, clk.Now)
	c.Set("a", 1)
	c.Set("b", 2)
	mustGet(t, c, "a", 1) // a теперь свежее b
	c.Set("c", 3)         // вытесняется b
	mustMiss(t, c, "b")
	mustGet(t, c, "a", 1)
	mustGet(t, c, "c", 3)
	if n := c.Len(); n != 2 {
		t.Errorf("Len = %d, want 2", n)
	}
}

func TestTTL(t *testing.T) {
	clk := newClock()
	c := New[string, string](10, time.Minute, clk.Now)
	c.Set("sku-1", "яблоки")
	clk.Advance(30 * time.Second)
	c.Set("sku-2", "груши")
	clk.Advance(30 * time.Second) // sku-1 прожил ровно минуту
	mustMiss(t, c, "sku-1")
	mustGet(t, c, "sku-2", "груши")
	if n := c.Len(); n != 1 {
		t.Errorf("Len = %d, want 1 (истёкшие не считаются)", n)
	}
}

func TestDeleteAndPurge(t *testing.T) {
	clk := newClock()
	c := New[int, int](10, time.Second, clk.Now)
	for i := range 5 {
		c.Set(i, i*i)
	}
	c.Delete(0)
	mustMiss(t, c, 0)
	clk.Advance(time.Second)
	if n := c.Purge(); n != 4 {
		t.Errorf("Purge = %d, want 4", n)
	}
	if n := c.Len(); n != 0 {
		t.Errorf("Len = %d, want 0", n)
	}
}
