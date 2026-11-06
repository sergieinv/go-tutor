package task

import (
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Все вызовы f должны идти одновременно: каждый ждёт, пока
// запустятся остальные. При последовательном выполнении
// первый вызов дождётся только таймаута.
func TestParallelMapConcurrent(t *testing.T) {
	const n = 50
	in := make([]int, n)
	for i := range in {
		in[i] = i
	}
	var ready sync.WaitGroup
	ready.Add(n)
	all := make(chan struct{})
	go func() {
		ready.Wait()
		close(all)
	}()
	var failed atomic.Bool
	got := ParallelMap(in, func(x int) int {
		ready.Done()
		if !failed.Load() {
			select {
			case <-all:
			case <-time.After(2 * time.Second):
				failed.Store(true)
			}
		}
		return x + 1
	})
	if failed.Load() {
		t.Fatal("вызовы f не выполнялись одновременно: каждый должен идти в своей горутине")
	}
	if len(got) != n {
		t.Fatalf("len(out) = %d, want %d", len(got), n)
	}
	for i, v := range got {
		if v != i+1 {
			t.Fatalf("out[%d] = %d, want %d", i, v, i+1)
		}
	}
}

// Поздние элементы завершаются раньше ранних — порядок
// результата всё равно должен совпадать с порядком входа.
func TestParallelMapKeepsOrder(t *testing.T) {
	in := []int{5, 4, 3, 2, 1}
	got := ParallelMap(in, func(x int) int {
		time.Sleep(time.Duration(x) * 5 * time.Millisecond)
		return x * 10
	})
	want := []int{50, 40, 30, 20, 10}
	if !slices.Equal(got, want) {
		t.Errorf("ParallelMap = %v, want %v", got, want)
	}
}

func TestParallelMapWaitsForAll(t *testing.T) {
	var finished atomic.Int32
	in := []int{1, 2, 3, 4, 5, 6, 7, 8}
	ParallelMap(in, func(x int) int {
		time.Sleep(time.Duration(x) * 3 * time.Millisecond)
		finished.Add(1)
		return x
	})
	if got := finished.Load(); got != int32(len(in)) {
		t.Errorf("ParallelMap вернулся, когда завершилось %d вызовов из %d", got, len(in))
	}
}

func TestParallelMapCallsOnce(t *testing.T) {
	var calls atomic.Int32
	in := make([]int, 100)
	got := ParallelMap(in, func(x int) int {
		calls.Add(1)
		return 7
	})
	if c := calls.Load(); c != 100 {
		t.Errorf("f вызвана %d раз, want 100", c)
	}
	if len(got) != 100 {
		t.Errorf("len(out) = %d, want 100", len(got))
	}
}

func TestParallelMapEmptySlice(t *testing.T) {
	got := ParallelMap([]int{}, func(x int) int {
		t.Error("f не должна вызываться для пустого входа")
		return x
	})
	if len(got) != 0 {
		t.Errorf("ParallelMap([]) = %v, want пустой срез", got)
	}
}
