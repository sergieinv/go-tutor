package task

import (
	"sync"
	"testing"
)

func TestCounterZeroValue(t *testing.T) {
	var c Counter
	if got := c.Value("x"); got != 0 {
		t.Errorf("Value у нулевого Counter = %d, want 0", got)
	}
	s := c.Snapshot()
	if s == nil || len(s) != 0 {
		t.Errorf("Snapshot у нулевого Counter = %#v, want пустую не-nil map", s)
	}
	c.Inc("x")
	if got := c.Value("x"); got != 1 {
		t.Errorf("Value(x) = %d, want 1", got)
	}
}

func TestCounterZeroValueConcurrent(t *testing.T) {
	// Первые Inc идут одновременно: ленивая инициализация map
	// тоже должна быть под защитой.
	var c Counter
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() { c.Inc("x") })
	}
	wg.Wait()
	if got := c.Value("x"); got != 20 {
		t.Errorf("Value(x) = %d, want 20", got)
	}
}

func TestSnapshotIsCopy(t *testing.T) {
	c := &Counter{}
	c.Inc("a")
	s := c.Snapshot()
	s["a"] = 100
	s["b"] = 5
	if got := c.Value("a"); got != 1 {
		t.Errorf("после изменения снимка Value(a) = %d, want 1: Snapshot должен возвращать копию", got)
	}
	if got := c.Value("b"); got != 0 {
		t.Errorf("после изменения снимка Value(b) = %d, want 0", got)
	}
	c.Inc("a")
	if s["a"] != 100 {
		t.Errorf("Inc изменил ранее выданный снимок")
	}
}

func TestSnapshotDuringInc(t *testing.T) {
	// Если Snapshot отдаёт внутреннюю map или читает её без
	// мьютекса, детектор гонок провалит этот тест.
	c := &Counter{}
	c.Inc("a")
	var wg sync.WaitGroup
	wg.Go(func() {
		for range 500 {
			c.Inc("a")
			c.Inc("b")
		}
	})
	wg.Go(func() {
		for range 200 {
			total := 0
			for _, v := range c.Snapshot() {
				total += v
			}
			_ = total
			_ = c.Value("b")
		}
	})
	wg.Wait()
	if got := c.Value("a"); got != 501 {
		t.Errorf("Value(a) = %d, want 501", got)
	}
}
