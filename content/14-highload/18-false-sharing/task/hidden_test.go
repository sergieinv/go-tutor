package task

import (
	"runtime"
	"sync"
	"testing"
	"unsafe"
)

// Счётчик лежит в начале шарда, а соседние счётчики в срезе
// отстоят друг от друга не меньше чем на линию.
func TestHiddenStride(t *testing.T) {
	if off := unsafe.Offsetof(shard{}.n); off != 0 {
		t.Fatalf("смещение n = %d, want 0", off)
	}
	c := New(4)
	if len(c.shards) != 4 {
		t.Fatalf("len(shards) = %d, want 4", len(c.shards))
	}
	a := uintptr(unsafe.Pointer(&c.shards[0].n))
	b := uintptr(unsafe.Pointer(&c.shards[1].n))
	if b-a < CacheLine {
		t.Fatalf("соседние счётчики на расстоянии %d байт, want >= %d", b-a, CacheLine)
	}
}

func TestHiddenDefaultShards(t *testing.T) {
	want := 1
	for want < runtime.GOMAXPROCS(0) {
		want *= 2
	}
	for _, n := range []int{0, -1, -100} {
		if got := New(n).Shards(); got != want {
			t.Errorf("New(%d).Shards() = %d, want %d (GOMAXPROCS = %d)", n, got, want, runtime.GOMAXPROCS(0))
		}
	}
	if got := New(1000).Shards(); got != 1024 {
		t.Errorf("New(1000).Shards() = %d, want 1024", got)
	}
}

func TestHiddenAddAtIndex(t *testing.T) {
	c := New(4)
	if len(c.shards) != 4 {
		t.Fatalf("len(shards) = %d, want 4", len(c.shards))
	}
	for _, tc := range []struct{ hint, idx int }{{0, 0}, {1, 1}, {6, 2}, {-1, 3}, {-4, 0}, {1 << 40, 0}} {
		before := c.shards[tc.idx].n.Load()
		c.AddAt(tc.hint, 7)
		if got := c.shards[tc.idx].n.Load() - before; got != 7 {
			t.Errorf("AddAt(%d, 7): шард %d изменился на %d, want 7", tc.hint, tc.idx, got)
		}
	}
	if got := c.Load(); got != 42 {
		t.Fatalf("Load() = %d, want 42", got)
	}
}

func TestHiddenSingleShard(t *testing.T) {
	c := New(1)
	if len(c.shards) != 1 {
		t.Fatalf("len(shards) = %d, want 1", len(c.shards))
	}
	c.AddAt(12345, 1)
	c.AddAt(-7, 1)
	c.Add(1)
	if c.Load() != 3 || c.shards[0].n.Load() != 3 {
		t.Fatalf("Load() = %d, want 3", c.Load())
	}
}

// Отрицательные и положительные приращения из многих горутин
// гасят друг друга точно.
func TestHiddenConcurrentMixed(t *testing.T) {
	c := New(0)
	var wg sync.WaitGroup
	for g := range 16 {
		wg.Go(func() {
			for i := range 1000 {
				if g%2 == 0 {
					c.Add(3)
				} else {
					c.AddAt(g*i, -3)
				}
			}
		})
	}
	wg.Wait()
	if got := c.Load(); got != 0 {
		t.Fatalf("Load() = %d, want 0", got)
	}
}

// При только положительных приращениях Load, вызванный позже,
// не меньше предыдущего.
func TestHiddenLoadMonotonic(t *testing.T) {
	c := New(8)
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					c.Add(1)
				}
			}
		})
	}
	prev := int64(0)
	for range 2000 {
		cur := c.Load()
		if cur < prev {
			close(stop)
			wg.Wait()
			t.Fatalf("Load уменьшился: %d после %d", cur, prev)
		}
		prev = cur
	}
	close(stop)
	wg.Wait()
	if c.Load() < prev {
		t.Fatal("итоговый Load меньше промежуточного")
	}
}
