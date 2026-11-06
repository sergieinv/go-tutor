package task

import (
	"fmt"
	"maps"
	"sync"
	"testing"
)

func TestCounterSequential(t *testing.T) {
	c := &Counter{}
	c.Inc("a")
	c.Inc("a")
	c.Inc("b")
	if got := c.Value("a"); got != 2 {
		t.Errorf("Value(a) = %d, want 2", got)
	}
	if got := c.Value("missing"); got != 0 {
		t.Errorf("Value(missing) = %d, want 0", got)
	}
	want := map[string]int{"a": 2, "b": 1}
	if got := c.Snapshot(); !maps.Equal(got, want) {
		t.Errorf("Snapshot() = %v, want %v", got, want)
	}
}

func TestCounterConcurrentInc(t *testing.T) {
	c := &Counter{}
	var wg sync.WaitGroup
	for g := range 50 {
		wg.Go(func() {
			for i := range 100 {
				c.Inc(fmt.Sprint("k", (g+i)%5))
			}
		})
	}
	wg.Wait()
	for k := range 5 {
		key := fmt.Sprint("k", k)
		if got := c.Value(key); got != 1000 {
			t.Errorf("Value(%s) = %d, want 1000: инкременты потерялись", key, got)
		}
	}
}
