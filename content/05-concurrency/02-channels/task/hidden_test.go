package task

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Все источники должны выполняться одновременно: каждый ждёт,
// пока запустятся остальные.
func TestSumAllConcurrent(t *testing.T) {
	const n = 40
	var ready sync.WaitGroup
	ready.Add(n)
	all := make(chan struct{})
	go func() {
		ready.Wait()
		close(all)
	}()
	var failed atomic.Bool
	sources := make([]func() int, n)
	for i := range sources {
		sources[i] = func() int {
			ready.Done()
			if !failed.Load() {
				select {
				case <-all:
				case <-time.After(2 * time.Second):
					failed.Store(true)
				}
			}
			return i
		}
	}
	got := SumAll(sources)
	if failed.Load() {
		t.Fatal("источники не выполнялись одновременно: каждый должен идти в своей горутине")
	}
	if want := n * (n - 1) / 2; got != want {
		t.Errorf("SumAll = %d, want %d", got, want)
	}
}

func TestSumAllWaitsAndNoLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	var finished, calls atomic.Int32
	sources := make([]func() int, 20)
	for i := range sources {
		sources[i] = func() int {
			calls.Add(1)
			time.Sleep(time.Duration(i%5) * 2 * time.Millisecond)
			finished.Add(1)
			return 1
		}
	}
	if got := SumAll(sources); got != 20 {
		t.Errorf("SumAll = %d, want 20", got)
	}
	if f := finished.Load(); f != 20 {
		t.Errorf("SumAll вернулся, когда завершилось %d источников из 20", f)
	}
	if c := calls.Load(); c != 20 {
		t.Errorf("источники вызваны %d раз, want 20", c)
	}
	// Горутины, запущенные SumAll, должны завершиться.
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("после SumAll осталось %d лишних горутин", n-before)
	}
}

func TestSumAllEmpty(t *testing.T) {
	if got := SumAll([]func() int{}); got != 0 {
		t.Errorf("SumAll([]) = %d, want 0", got)
	}
}
