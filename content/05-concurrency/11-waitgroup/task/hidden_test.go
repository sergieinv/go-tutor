package task

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// waitGoroutines ждёт, пока число горутин опустится до base.
func waitGoroutines(base int) int {
	deadline := time.Now().Add(time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= base || time.Now().After(deadline) {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// async выполняет f и ждёт её не дольше 2 с.
func async[T any](t *testing.T, what string, f func() T) T {
	t.Helper()
	done := make(chan T, 1)
	go func() { done <- f() }()
	select {
	case v := <-done:
		return v
	case <-time.After(2 * time.Second):
		t.Fatalf("%s не вернулась за 2 с", what)
		panic("unreachable")
	}
}

func TestWaitTimeoutZeroCounter(t *testing.T) {
	var wg sync.WaitGroup
	if !async(t, "WaitTimeout", func() bool { return WaitTimeout(&wg, time.Hour) }) {
		t.Fatal("WaitTimeout на нулевом счётчике = false, want true")
	}
}

func TestWaitTimeoutReturnsEarly(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		time.Sleep(10 * time.Millisecond)
		wg.Done()
	}()
	ok := async(t, "WaitTimeout", func() bool { return WaitTimeout(&wg, time.Hour) })
	if !ok {
		t.Fatal("WaitTimeout = false, want true")
	}

	// После обнуления wg можно использовать снова.
	wg.Add(1)
	go wg.Done()
	if !async(t, "WaitTimeout", func() bool { return WaitTimeout(&wg, time.Hour) }) {
		t.Fatal("WaitTimeout на переиспользованном wg = false, want true")
	}
}

func TestWaitTimeoutNotBeforeDeadline(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()
	const d = 30 * time.Millisecond
	start := time.Now()
	ok := async(t, "WaitTimeout", func() bool { return WaitTimeout(&wg, d) })
	if ok {
		t.Fatal("WaitTimeout = true, want false")
	}
	if el := time.Since(start); el < d {
		t.Fatalf("WaitTimeout вернула false через %v, раньше таймаута %v", el, d)
	}
}

// После таймаута помощник должен завершиться, когда счётчик
// дойдёт до нуля, а сам счётчик WaitTimeout не меняет.
func TestWaitTimeoutNoLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	var wg sync.WaitGroup
	wg.Add(2)
	if WaitTimeout(&wg, 10*time.Millisecond) {
		t.Fatal("WaitTimeout = true, want false")
	}
	if WaitTimeout(&wg, 10*time.Millisecond) {
		t.Fatal("повторный WaitTimeout = true, want false")
	}
	wg.Done()
	wg.Done()
	async(t, "wg.Wait после WaitTimeout", func() bool { wg.Wait(); return true })
	if n := waitGoroutines(base); n > base {
		t.Fatalf("лишних горутин: %d — помощник WaitTimeout не завершился после обнуления счётчика", n-base)
	}

}

func TestRunAllEmpty(t *testing.T) {
	errs, ok := RunAll(time.Hour)
	if !ok || len(errs) != 0 {
		t.Fatalf("RunAll() = (%v, %v), want (срез длины 0, true)", errs, ok)
	}
}

// Все задачи должны идти одновременно: каждая ждёт, пока
// стартуют остальные.
func TestRunAllConcurrent(t *testing.T) {
	const n = 30
	var started atomic.Int32
	all := make(chan struct{})
	var calls [n]atomic.Int32
	tasks := make([]func() error, n)
	for i := range tasks {
		tasks[i] = func() error {
			calls[i].Add(1)
			if started.Add(1) == n {
				close(all)
			}
			select {
			case <-all:
			case <-time.After(3 * time.Second):
				return errors.New("timeout")
			}
			if i%3 == 0 {
				return errors.New("odd")
			}
			return nil
		}
	}
	type res struct {
		errs []error
		ok   bool
	}
	r := async(t, "RunAll", func() res {
		errs, ok := RunAll(time.Hour, tasks...)
		return res{errs, ok}
	})
	if !r.ok || len(r.errs) != n {
		t.Fatalf("RunAll вернул ok = %v, len = %d; want true, %d", r.ok, len(r.errs), n)
	}
	for i, err := range r.errs {
		if c := calls[i].Load(); c != 1 {
			t.Fatalf("задача %d вызвана %d раз, want 1", i, c)
		}
		if err != nil && err.Error() == "timeout" {
			t.Fatal("задачи выполнялись не одновременно: каждая должна идти в своей горутине")
		}
		if (i%3 == 0) != (err != nil) {
			t.Fatalf("errs[%d] = %v: ошибка не на своём месте", i, err)
		}
	}
}

func TestRunAllWaitsForAll(t *testing.T) {
	var finished atomic.Int32
	tasks := make([]func() error, 6)
	for i := range tasks {
		tasks[i] = func() error {
			time.Sleep(time.Duration(i) * 3 * time.Millisecond)
			finished.Add(1)
			return nil
		}
	}
	if _, ok := RunAll(5*time.Second, tasks...); !ok {
		t.Fatal("RunAll вернул ok = false, want true")
	}
	if n := finished.Load(); n != int32(len(tasks)) {
		t.Fatalf("RunAll вернулся, когда завершилось %d задач из %d", n, len(tasks))
	}
}

// По таймауту RunAll возвращает nil — срез, в который задачи ещё
// пишут, наружу отдавать нельзя, — а горутины задач и помощника
// завершаются вместе с задачами.
func TestRunAllTimeout(t *testing.T) {
	base := runtime.NumGoroutine()
	release := make(chan struct{})
	slow := func() error { <-release; return errors.New("late") }
	const d = 30 * time.Millisecond
	start := time.Now()
	type res struct {
		errs []error
		ok   bool
	}
	r := async(t, "RunAll", func() res {
		errs, ok := RunAll(d, func() error { return nil }, slow, slow)
		return res{errs, ok}
	})
	el := time.Since(start)
	close(release)
	if r.ok || r.errs != nil {
		t.Fatalf("RunAll по таймауту = (%v, %v), want (nil, false)", r.errs, r.ok)
	}
	if el < d {
		t.Fatalf("RunAll вернул false через %v, раньше таймаута %v", el, d)
	}
	if n := waitGoroutines(base); n > base {
		t.Fatalf("лишних горутин после завершения задач: %d", n-base)
	}
}
