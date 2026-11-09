package task

import (
	"sync"
	"testing"
	"unsafe"
)

func TestShardLayout(t *testing.T) {
	if got := unsafe.Sizeof(shard{}); got != CacheLine {
		t.Fatalf("unsafe.Sizeof(shard{}) = %d, want %d", got, CacheLine)
	}
}

func TestShardsRoundUp(t *testing.T) {
	for _, tc := range []struct{ n, want int }{{1, 1}, {2, 2}, {3, 4}, {5, 8}, {8, 8}, {9, 16}} {
		if got := New(tc.n).Shards(); got != tc.want {
			t.Errorf("New(%d).Shards() = %d, want %d", tc.n, got, tc.want)
		}
	}
}

func TestAddLoad(t *testing.T) {
	c := New(4)
	c.Add(5)
	c.AddAt(2, 10)
	c.Add(-3)
	if got := c.Load(); got != 12 {
		t.Fatalf("Load() = %d, want 12", got)
	}
}

func TestConcurrentAdd(t *testing.T) {
	c := New(8)
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for range 2000 {
				c.Add(1)
				c.AddAt(g, 2)
			}
		})
	}
	wg.Wait()
	if got := c.Load(); got != 8*2000*3 {
		t.Fatalf("Load() = %d, want %d", got, 8*2000*3)
	}
}
