package task

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// waitFor ждёт условия до deadline, проверяя его каждую миллисекунду.
func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func TestMapConcurrencyLimit(t *testing.T) {
	const workers = 3
	var active, peak atomic.Int32
	release := make(chan struct{})
	f := func(x int) int {
		n := active.Add(1)
		for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
		}
		<-release
		active.Add(-1)
		return x
	}

	done := make(chan []int)
	in := []int{0, 1, 2, 3, 4, 5, 6, 7, 8, 9}
	go func() { done <- Map(in, workers, f) }()

	// Ждём, пока workers вызовов f выполняются одновременно.
	if !waitFor(2*time.Second, func() bool { return active.Load() == workers }) {
		close(release)
		<-done
		t.Fatalf("одновременно выполняется %d вызовов f, want %d: воркеры должны работать параллельно", active.Load(), workers)
	}
	// Даём шанс лишним горутинам войти в f, если лимита нет.
	time.Sleep(20 * time.Millisecond)
	close(release)
	got := <-done
	if p := peak.Load(); p > workers {
		t.Errorf("одновременно выполнялось %d вызовов f, want не больше %d", p, workers)
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("out[%d] = %d, want %d", i, v, i)
		}
	}
}

func TestMapCallsOnce(t *testing.T) {
	var calls [50]atomic.Int32
	in := make([]int, len(calls))
	for i := range in {
		in[i] = i
	}
	Map(in, 4, func(x int) struct{} {
		calls[x].Add(1)
		return struct{}{}
	})
	for i := range calls {
		if n := calls[i].Load(); n != 1 {
			t.Errorf("f(%d) вызвана %d раз, want 1", i, n)
		}
	}
}

func TestMapEmpty(t *testing.T) {
	called := false
	f := func(x int) int { called = true; return x }
	for _, in := range [][]int{nil, {}} {
		got := Map(in, 3, f)
		if got == nil || len(got) != 0 {
			t.Errorf("Map(%#v) = %#v, want пустой не-nil срез", in, got)
		}
	}
	if called {
		t.Error("f вызвана для пустого входа")
	}
}

func TestMapNonPositiveWorkers(t *testing.T) {
	for _, workers := range []int{0, -5} {
		var active, peak atomic.Int32
		got := Map([]int{1, 2, 3, 4}, workers, func(x int) int {
			n := active.Add(1)
			for p := peak.Load(); n > p && !peak.CompareAndSwap(p, n); p = peak.Load() {
			}
			time.Sleep(time.Millisecond)
			active.Add(-1)
			return x * 10
		})
		if len(got) != 4 || got[0] != 10 || got[3] != 40 {
			t.Errorf("Map(workers=%d) = %v, want [10 20 30 40]", workers, got)
		}
		if p := peak.Load(); p > 1 {
			t.Errorf("Map(workers=%d): одновременно %d вызовов f, want один воркер", workers, p)
		}
	}
}

func TestMapNoLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 20 {
		Map([]int{1, 2, 3, 4, 5}, 4, func(x int) int { return x })
	}
	if !waitFor(time.Second, func() bool { return runtime.NumGoroutine() <= before }) {
		t.Errorf("горутин до: %d, после: %d — Map оставляет горутины после возврата", before, runtime.NumGoroutine())
	}
}
