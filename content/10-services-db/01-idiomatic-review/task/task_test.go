package task

import (
	"context"
	"sync"
	"testing"
	"time"
)

// fakeSource — цены в памяти; считает обращения и запоминает
// контекст последнего вызова.
type fakeSource struct {
	mu      sync.Mutex
	prices  map[string]int64
	calls   int
	lastCtx context.Context
}

func newSource() *fakeSource {
	return &fakeSource{prices: map[string]int64{"apple": 120, "pear": 90, "plum": 45}}
}

func (s *fakeSource) Price(ctx context.Context, sku string) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	s.lastCtx = ctx
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	p, ok := s.prices[sku]
	if !ok {
		return 0, ErrNotFound
	}
	return p, nil
}

func (s *fakeSource) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

func newCatalog(t *testing.T, src Source, ttl time.Duration) *Catalog {
	t.Helper()
	c := NewCatalog(src, ttl)
	t.Cleanup(c.Close)
	return c
}

func TestPrice(t *testing.T) {
	c := newCatalog(t, newSource(), time.Hour)
	for sku, want := range map[string]int64{"apple": 120, "pear": 90} {
		if got, err := c.Price(t.Context(), sku); got != want || err != nil {
			t.Errorf("Price(%q) = (%d, %v), want (%d, nil)", sku, got, err, want)
		}
	}
}

func TestPriceCached(t *testing.T) {
	src := newSource()
	c := newCatalog(t, src, time.Hour)
	for range 3 {
		if got, err := c.Price(t.Context(), "apple"); got != 120 || err != nil {
			t.Fatalf("Price(apple) = (%d, %v), want (120, nil)", got, err)
		}
	}
	if n := src.callCount(); n != 1 {
		t.Errorf("три Price(apple) обратились к источнику %d раз, want 1", n)
	}
}

func TestCacheCleared(t *testing.T) {
	src := newSource()
	c := newCatalog(t, src, 5*time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for src.callCount() < 2 {
		if time.Now().After(deadline) {
			t.Fatal("кеш не очистился за 5 с при ttl = 5 мс")
		}
		if _, err := c.Price(t.Context(), "pear"); err != nil {
			t.Fatalf("Price(pear) = %v", err)
		}
		time.Sleep(time.Millisecond)
	}
}

func TestTotal(t *testing.T) {
	c := newCatalog(t, newSource(), time.Hour)
	tests := []struct {
		skus []string
		want int64
	}{
		{nil, 0},
		{[]string{"apple"}, 120},
		{[]string{"apple", "pear", "plum", "apple"}, 375},
	}
	for _, tt := range tests {
		if got, err := c.Total(t.Context(), tt.skus); got != tt.want || err != nil {
			t.Errorf("Total(%q) = (%d, %v), want (%d, nil)", tt.skus, got, err, tt.want)
		}
	}
}
