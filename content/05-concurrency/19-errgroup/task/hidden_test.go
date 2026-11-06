package task

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

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

func TestProcessAllFirstErrorNotCanceled(t *testing.T) {
	boom := errors.New("boom")
	var canceled atomic.Int32
	f := func(ctx context.Context, s string) (int, error) {
		if s == "boom" {
			time.Sleep(10 * time.Millisecond)
			return 0, boom
		}
		<-ctx.Done() // остальные ждут отмены
		canceled.Add(1)
		return 0, ctx.Err()
	}
	_, err := ProcessAll(context.Background(), []string{"a", "b", "boom", "c"}, 0, f)
	if !errors.Is(err, boom) {
		t.Fatalf("ProcessAll вернул %v, want первую ошибку boom, а не ошибку отменённых", err)
	}
	// Сюда дошли — значит, ждавшие вызовы получили отмену и вышли.
	if n := canceled.Load(); n > 3 {
		t.Errorf("отмену увидели %d вызовов, want не больше 3", n)
	}
}

func TestProcessAllLimit(t *testing.T) {
	var active, peak atomic.Int32
	var mu sync.Mutex
	f := func(_ context.Context, s string) (int, error) {
		n := active.Add(1)
		mu.Lock()
		if n > peak.Load() {
			peak.Store(n)
		}
		mu.Unlock()
		time.Sleep(5 * time.Millisecond)
		active.Add(-1)
		return len(s), nil
	}
	items := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	if _, err := ProcessAll(context.Background(), items, 3, f); err != nil {
		t.Fatalf("ProcessAll вернул %v", err)
	}
	if p := peak.Load(); p > 3 {
		t.Errorf("одновременно выполнялось %d вызовов f, want не больше 3", p)
	}
}

func TestProcessAllNegativeLimitIsUnlimited(t *testing.T) {
	const n = 5
	var started sync.WaitGroup
	started.Add(n)
	f := func(_ context.Context, s string) (int, error) {
		started.Done()
		started.Wait() // все n вызовов должны идти одновременно
		return 1, nil
	}
	for _, limit := range []int{0, -1} {
		started = sync.WaitGroup{}
		started.Add(n)
		done := make(chan error, 1)
		go func() {
			_, err := ProcessAll(context.Background(), make([]string, n), limit, f)
			done <- err
		}()
		select {
		case err := <-done:
			if err != nil {
				t.Fatalf("ProcessAll(limit=%d) вернул %v", limit, err)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("ProcessAll(limit=%d) завис: при limit <= 0 ограничения нет", limit)
		}
	}
}

func TestProcessAllSkipsAfterError(t *testing.T) {
	boom := errors.New("boom")
	var mu sync.Mutex
	var called []string
	f := func(_ context.Context, s string) (int, error) {
		mu.Lock()
		called = append(called, s)
		mu.Unlock()
		if s == "c" {
			return 0, boom
		}
		return 1, nil
	}
	_, err := ProcessAll(context.Background(), []string{"a", "b", "c", "d", "e"}, 1, f)
	if !errors.Is(err, boom) {
		t.Fatalf("ProcessAll вернул %v, want boom", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(called) != 3 {
		t.Errorf("f вызвана для %v, want только a, b, c: после ошибки новые вызовы не начинаются", called)
	}
}

func TestProcessAllParentCanceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := func(ctx context.Context, s string) (int, error) {
		if s == "trigger" {
			cancel()
		}
		<-ctx.Done()
		return 0, ctx.Err()
	}
	got, err := ProcessAll(ctx, []string{"x", "trigger", "y"}, 0, f)
	if !errors.Is(err, context.Canceled) || got != nil {
		t.Fatalf("ProcessAll при отмене ctx = (%v, %v), want (nil, context.Canceled)", got, err)
	}
}

func TestProcessAllEmpty(t *testing.T) {
	for _, items := range [][]string{nil, {}} {
		got, err := ProcessAll(context.Background(), items, 2, length)
		if err != nil || got == nil || len(got) != 0 {
			t.Errorf("ProcessAll(%#v) = (%#v, %v), want пустой не-nil срез и nil", items, got, err)
		}
	}
}

func TestProcessAllNoLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 10 {
		ProcessAll(context.Background(), []string{"a", "b", "c"}, 2, length)
	}
	if !waitFor(time.Second, func() bool { return runtime.NumGoroutine() <= before }) {
		t.Errorf("горутин до: %d, после: %d", before, runtime.NumGoroutine())
	}
}

// ctx отменён до вызова: f не начинается ни для одного элемента.
func TestProcessAllParentCanceledBefore(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	for _, limit := range []int{0, 1} {
		var calls atomic.Int32
		f := func(context.Context, string) (int, error) {
			calls.Add(1)
			return 1, nil
		}
		got, err := ProcessAll(ctx, []string{"a", "b", "c"}, limit, f)
		if !errors.Is(err, context.Canceled) || got != nil {
			t.Errorf("limit=%d: ProcessAll(отменённый ctx) = (%v, %v), want (nil, context.Canceled)", limit, got, err)
		}
		if n := calls.Load(); n != 0 {
			t.Errorf("limit=%d: f вызвана %d раз, want 0: ctx отменён до вызова", limit, n)
		}
	}
}
