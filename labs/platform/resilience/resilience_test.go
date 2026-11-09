package resilience

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

var (
	errTemporary = errors.New("temporary")
	errFatal     = errors.New("fatal")
)

// fakeClock — часы, которые идут только по команде. Sleep продвигает их
// мгновенно: тест проверяет паузы, не ожидая их.
type fakeClock struct {
	mu     sync.Mutex
	now    time.Time
	sleeps []time.Duration
}

func newFakeClock() *fakeClock {
	return &fakeClock{now: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	c.mu.Lock()
	c.sleeps = append(c.sleeps, d)
	c.mu.Unlock()
	c.Advance(d)
	return nil
}

func newRetry(clock *fakeClock) Retry {
	return Retry{
		MaxAttempts: 4,
		BaseDelay:   100 * time.Millisecond,
		MaxDelay:    250 * time.Millisecond,
		Retryable:   func(err error) bool { return errors.Is(err, errTemporary) },
		Sleep:       clock.Sleep,
		Now:         clock.Now,
		// Без случайности: пауза — верхняя граница.
		Jitter: func(max time.Duration) time.Duration { return max },
	}
}

func TestRetryBacksOffExponentiallyUpToMax(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	err := newRetry(clock).Do(context.Background(), func(context.Context) error {
		calls++
		return errTemporary
	})
	if !errors.Is(err, errTemporary) {
		t.Fatalf("err = %v, want errTemporary", err)
	}
	if calls != 4 {
		t.Errorf("calls = %d, want 4", calls)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 250 * time.Millisecond}
	if !slices.Equal(clock.sleeps, want) {
		t.Errorf("sleeps = %v, want %v", clock.sleeps, want)
	}
}

func TestRetryStopsOnSuccessAndFatalError(t *testing.T) {
	clock := newFakeClock()
	calls := 0
	err := newRetry(clock).Do(context.Background(), func(context.Context) error {
		calls++
		if calls == 2 {
			return nil
		}
		return errTemporary
	})
	if err != nil || calls != 2 {
		t.Errorf("success on 2nd attempt: err = %v, calls = %d", err, calls)
	}

	calls = 0
	err = newRetry(clock).Do(context.Background(), func(context.Context) error {
		calls++
		return errFatal
	})
	if !errors.Is(err, errFatal) || calls != 1 {
		t.Errorf("fatal error: err = %v, calls = %d, want errFatal after 1 call", err, calls)
	}
}

func TestRetryRespectsDeadline(t *testing.T) {
	// Дедлайн контекста — настоящее время (далеко в будущем, чтобы не
	// истёк сам), а часы Retry стоят за 250 мс до него: первая пауза (100)
	// помещается, вторая (200) — уже нет.
	deadline := time.Now().Add(time.Hour)
	clock := &fakeClock{now: deadline.Add(-250 * time.Millisecond)}
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	calls := 0
	err := newRetry(clock).Do(ctx, func(context.Context) error {
		calls++
		return errTemporary
	})
	if !errors.Is(err, errTemporary) || calls != 2 {
		t.Errorf("err = %v, calls = %d; want errTemporary after 2 calls", err, calls)
	}
}

func TestRetryReturnsOnCanceledContext(t *testing.T) {
	clock := newFakeClock()
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := newRetry(clock).Do(ctx, func(context.Context) error {
		calls++
		cancel()
		return errTemporary
	})
	if !errors.Is(err, errTemporary) || calls != 1 {
		t.Errorf("err = %v, calls = %d; want errTemporary after 1 call", err, calls)
	}
}

func TestRetryJitterWithinBounds(t *testing.T) {
	r := Retry{}
	for range 1000 {
		if d := r.jitter(10 * time.Millisecond); d < 0 || d > 10*time.Millisecond {
			t.Fatalf("jitter = %v, want [0, 10ms]", d)
		}
	}
	if d := r.jitter(0); d != 0 {
		t.Errorf("jitter(0) = %v", d)
	}
}

type transition struct{ from, to State }

func newBreaker(clock *fakeClock, log *[]transition) *Breaker {
	return NewBreaker(BreakerConfig{
		FailureThreshold: 3,
		OpenTimeout:      time.Second,
		IsFailure:        func(err error) bool { return errors.Is(err, errTemporary) },
		Now:              clock.Now,
		OnStateChange:    func(from, to State) { *log = append(*log, transition{from, to}) },
	})
}

func call(b *Breaker, err error) error {
	return b.Do(context.Background(), func(context.Context) error { return err })
}

func TestBreakerOpensAfterConsecutiveFailures(t *testing.T) {
	clock := newFakeClock()
	var log []transition
	b := newBreaker(clock, &log)

	// Успех сбрасывает счётчик: 2 неудачи, успех, 2 неудачи — ещё замкнут.
	for _, err := range []error{errTemporary, errTemporary, nil, errTemporary, errTemporary} {
		_ = call(b, err)
	}
	// Ошибка клиента — не неудача соседа.
	_ = call(b, errFatal)
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}
	_ = call(b, errTemporary)
	_ = call(b, errTemporary)
	_ = call(b, errTemporary)
	if b.State() != StateOpen {
		t.Fatalf("state = %v, want open", b.State())
	}

	called := false
	err := b.Do(context.Background(), func(context.Context) error { called = true; return nil })
	if !errors.Is(err, ErrOpen) || called {
		t.Errorf("open breaker: err = %v, called = %v; want ErrOpen without call", err, called)
	}
	if want := []transition{{StateClosed, StateOpen}}; !slices.Equal(log, want) {
		t.Errorf("transitions = %v, want %v", log, want)
	}
}

func TestBreakerHalfOpenProbe(t *testing.T) {
	clock := newFakeClock()
	var log []transition
	b := newBreaker(clock, &log)
	for range 3 {
		_ = call(b, errTemporary)
	}

	clock.Advance(999 * time.Millisecond)
	if err := call(b, nil); !errors.Is(err, ErrOpen) {
		t.Fatalf("before OpenTimeout: err = %v, want ErrOpen", err)
	}

	// Пробный вызов не удался — снова open на OpenTimeout.
	clock.Advance(time.Millisecond)
	if err := call(b, errTemporary); !errors.Is(err, errTemporary) {
		t.Fatalf("probe: err = %v", err)
	}
	if err := call(b, nil); !errors.Is(err, ErrOpen) {
		t.Fatalf("after failed probe: err = %v, want ErrOpen", err)
	}

	// Пробный вызов удался — closed.
	clock.Advance(time.Second)
	if err := call(b, nil); err != nil {
		t.Fatalf("probe: err = %v", err)
	}
	if b.State() != StateClosed {
		t.Fatalf("state = %v, want closed", b.State())
	}
	want := []transition{
		{StateClosed, StateOpen},
		{StateOpen, StateHalfOpen}, {StateHalfOpen, StateOpen},
		{StateOpen, StateHalfOpen}, {StateHalfOpen, StateClosed},
	}
	if !slices.Equal(log, want) {
		t.Errorf("transitions = %v, want %v", log, want)
	}
}

func TestBreakerSingleProbeAtATime(t *testing.T) {
	clock := newFakeClock()
	var log []transition
	b := newBreaker(clock, &log)
	for range 3 {
		_ = call(b, errTemporary)
	}
	clock.Advance(time.Second)

	// Пока пробный вызов идёт, остальные отклоняются.
	probeStarted, release := make(chan struct{}), make(chan struct{})
	done := make(chan error)
	go func() {
		done <- b.Do(context.Background(), func(context.Context) error {
			close(probeStarted)
			<-release
			return nil
		})
	}()
	<-probeStarted
	if err := call(b, nil); !errors.Is(err, ErrOpen) {
		t.Errorf("concurrent call during probe: err = %v, want ErrOpen", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatalf("probe: %v", err)
	}
	if b.State() != StateClosed {
		t.Errorf("state = %v, want closed", b.State())
	}
}

func TestBreakerPanicCountsAsFailedProbe(t *testing.T) {
	clock := newFakeClock()
	b := NewBreaker(BreakerConfig{FailureThreshold: 1, OpenTimeout: time.Second, Now: clock.Now})
	_ = call(b, errTemporary)
	clock.Advance(time.Second)
	func() {
		defer func() { _ = recover() }()
		_ = b.Do(context.Background(), func(context.Context) error { panic("boom") })
	}()
	if b.State() != StateOpen {
		t.Fatalf("state after panicking probe = %v, want open", b.State())
	}
}

func TestBreakerConcurrentUse(t *testing.T) {
	b := NewBreaker(BreakerConfig{FailureThreshold: 5, OpenTimeout: time.Millisecond})
	var wg sync.WaitGroup
	for i := range 16 {
		wg.Go(func() {
			for j := range 200 {
				err := error(nil)
				if (i+j)%3 == 0 {
					err = errTemporary
				}
				_ = call(b, err)
				_ = b.State()
			}
		})
	}
	wg.Wait()
}
