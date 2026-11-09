package task

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeAfter — управляемый таймер очереди: тест сам решает, когда «истекло T».
type fakeAfter struct {
	mu    sync.Mutex
	durs  []time.Duration
	chans []chan time.Time
}

func (f *fakeAfter) After(d time.Duration) <-chan time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	ch := make(chan time.Time, 1)
	f.durs = append(f.durs, d)
	f.chans = append(f.chans, ch)
	return ch
}

func (f *fakeAfter) calls() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.durs...)
}

// fire «истекает» i-й по счёту таймер (дожидается, пока его заведут).
func (f *fakeAfter) fire(t *testing.T, i int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		if i < len(f.chans) {
			ch := f.chans[i]
			f.mu.Unlock()
			ch <- time.Time{}
			return
		}
		f.mu.Unlock()
		if time.Now().After(deadline) {
			t.Fatalf("таймер очереди №%d не заведён: After вызван %d раз", i+1, len(f.calls()))
		}
		time.Sleep(time.Millisecond)
	}
}

// call — вызов Do в отдельной горутине; fn ждёт команды завершиться.
type call struct {
	started chan struct{}
	release chan error
	done    chan error
}

func begin(l *Limiter, ctx context.Context) *call {
	c := &call{started: make(chan struct{}), release: make(chan error, 1), done: make(chan error, 1)}
	go func() {
		c.done <- l.Do(ctx, func(context.Context) error {
			close(c.started)
			return <-c.release
		})
	}()
	return c
}

// running запускает вызов и ждёт, что fn действительно началась.
func running(t *testing.T, l *Limiter) *call {
	t.Helper()
	c := begin(l, context.Background())
	select {
	case <-c.started:
	case err := <-c.done:
		t.Fatalf("Do вернул %v, не вызвав fn, хотя место было", err)
	case <-time.After(2 * time.Second):
		t.Fatal("fn не вызвана за 2 с, хотя место было")
	}
	return c
}

// queued запускает вызов и ждёт, что он встал в очередь (Stats().Queued == n).
func queued(t *testing.T, l *Limiter, ctx context.Context, n int) *call {
	t.Helper()
	c := begin(l, ctx)
	waitFor(t, l, fmt.Sprintf("Queued == %d", n), func(s Stats) bool { return s.Queued == n })
	select {
	case <-c.started:
		t.Fatal("fn вызвана, хотя все места заняты — вызов должен ждать в очереди")
	case err := <-c.done:
		t.Fatalf("Do вернул %v, а должен ждать в очереди", err)
	default:
	}
	return c
}

func (c *call) finish(t *testing.T, res error) error {
	t.Helper()
	c.release <- res
	return c.wait(t)
}

func (c *call) wait(t *testing.T) error {
	t.Helper()
	select {
	case err := <-c.done:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("Do не вернулся за 2 с")
		return nil
	}
}

func waitFor(t *testing.T, l *Limiter, what string, ok func(Stats) bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !ok(l.Stats()) {
		if time.Now().After(deadline) {
			t.Fatalf("за 2 с не дождались %s: Stats = %+v", what, l.Stats())
		}
		time.Sleep(time.Millisecond)
	}
}

// begun ждёт, что вызов из очереди получил место и fn началась.
func (c *call) begun(t *testing.T) {
	t.Helper()
	select {
	case <-c.started:
	case err := <-c.done:
		t.Fatalf("Do вернул %v, а должен был получить освободившееся место", err)
	case <-time.After(2 * time.Second):
		t.Fatal("за 2 с вызов из очереди не получил освободившееся место")
	}
}

func TestImmediate(t *testing.T) {
	l := NewLimiter(Config{MaxConcurrent: 2, MaxQueue: 1, QueueTimeout: time.Second})
	called := false
	err := l.Do(context.Background(), func(context.Context) error { called = true; return nil })
	if err != nil || !called {
		t.Fatalf("Do = %v, fn вызвана: %v; want nil, true", err, called)
	}
	want := Stats{Accepted: 1}
	if s := l.Stats(); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
	boom := errors.New("boom")
	if err := l.Do(context.Background(), func(context.Context) error { return boom }); err != boom {
		t.Errorf("Do = %v, want ошибку fn как есть", err)
	}
}

func TestShedWhenQueueFull(t *testing.T) {
	fa := &fakeAfter{}
	l := NewLimiter(Config{MaxConcurrent: 2, MaxQueue: 2, QueueTimeout: 50 * time.Millisecond, After: fa.After})
	a, b := running(t, l), running(t, l)
	q1 := queued(t, l, context.Background(), 1)
	q2 := queued(t, l, context.Background(), 2)
	// Мест и очереди нет: отказ сразу, fn не вызывается.
	for range 2 {
		err := l.Do(context.Background(), func(context.Context) error {
			t.Error("fn вызвана при полной очереди")
			return nil
		})
		if !errors.Is(err, ErrOverloaded) {
			t.Fatalf("Do при полной очереди = %v, want ErrOverloaded", err)
		}
	}
	want := Stats{InFlight: 2, Queued: 2, Accepted: 2, Rejected: 2}
	if s := l.Stats(); s != want {
		t.Fatalf("Stats = %+v, want %+v", s, want)
	}
	// Освободилось место — его получает очередь.
	a.finish(t, nil)
	q1.begun(t)
	b.finish(t, nil)
	q2.begun(t)
	q1.finish(t, nil)
	q2.finish(t, nil)
	want = Stats{Accepted: 4, Rejected: 2}
	if s := l.Stats(); s != want {
		t.Errorf("в конце Stats = %+v, want %+v", s, want)
	}
}

func TestQueueTimeout(t *testing.T) {
	fa := &fakeAfter{}
	l := NewLimiter(Config{MaxConcurrent: 1, MaxQueue: 1, QueueTimeout: 50 * time.Millisecond, After: fa.After})
	a := running(t, l)
	q := queued(t, l, context.Background(), 1)
	fa.fire(t, 0)
	if err := q.wait(t); !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("Do после таймаута очереди = %v, want ErrQueueTimeout", err)
	}
	if d := fa.calls(); len(d) != 1 || d[0] != 50*time.Millisecond {
		t.Errorf("After вызван с %v, want [50ms]", d)
	}
	want := Stats{InFlight: 1, Accepted: 1, TimedOut: 1}
	if s := l.Stats(); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
	a.finish(t, nil)
}
