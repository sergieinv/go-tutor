package task

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

// inflight — вызов fn, который начался и ждёт команды завершиться.
type inflight struct {
	started chan struct{}
	release chan error
	done    chan error // что вернул Do
}

// start запускает b.Do в горутине и ждёт, пока fn действительно начнётся.
func start(t *testing.T, b *Breaker) *inflight {
	t.Helper()
	c := &inflight{started: make(chan struct{}), release: make(chan error), done: make(chan error, 1)}
	go func() {
		c.done <- b.Do(func() error {
			close(c.started)
			return <-c.release
		})
	}()
	select {
	case <-c.started:
	case err := <-c.done:
		t.Fatalf("Do вернул %v, не вызвав fn", err)
	case <-time.After(5 * time.Second):
		t.Fatal("fn не вызвана за 5 с")
	}
	return c
}

// finish отпускает вызов с результатом res и ждёт возврата Do.
func (c *inflight) finish(t *testing.T, res error) error {
	t.Helper()
	c.release <- res
	select {
	case err := <-c.done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("Do не вернулся за 5 с после завершения fn")
		return nil
	}
}

func TestHiddenDefaults(t *testing.T) {
	clk := newClock()
	// Порог и число проб меньше 1 — считаются как 1; IsFailure == nil — err != nil.
	b := New(Config{FailureThreshold: 0, OpenTimeout: time.Second, HalfOpenProbes: -3, Now: clk.Now})
	if err := b.Do(func() error { return errBad }); err != errBad {
		t.Fatalf("Do = %v, want %v", err, errBad)
	}
	if b.State() != Open {
		t.Fatalf("порог 0 → 1: State = %v, want open", b.State())
	}
	clk.Advance(time.Second)
	if err := b.Do(ok); err != nil || b.State() != Closed {
		t.Errorf("одна удачная проба: Do = %v, State = %v; want nil, closed", err, b.State())
	}
	if New(Config{OpenTimeout: time.Second}).State() != Closed {
		t.Error("новый breaker без Now должен быть closed")
	}
	if got := State(42).String(); got == "closed" || got == "open" || got == "half-open" {
		t.Errorf("State(42).String() = %q — не должно совпадать с настоящим состоянием", got)
	}
}

func TestHiddenRealClock(t *testing.T) {
	b := New(Config{FailureThreshold: 1, OpenTimeout: time.Hour})
	b.Do(fail)
	if b.State() != Open {
		t.Fatalf("Now == nil: State = %v, want open", b.State())
	}
	if err := b.Do(ok); err != ErrOpen {
		t.Errorf("Now == nil, таймаут час: Do = %v, want ErrOpen", err)
	}
}

func TestHiddenNonFailureErrorResets(t *testing.T) {
	isFailure := func(err error) bool { return errors.Is(err, errDown) }
	b := New(Config{FailureThreshold: 3, OpenTimeout: time.Second, HalfOpenProbes: 1, IsFailure: isFailure, Now: newClock().Now})
	b.Do(fail)
	b.Do(fail)
	b.Do(func() error { return errBad }) // ответ здорового соседа — сбрасывает счёт
	b.Do(fail)
	b.Do(fail)
	if b.State() != Closed {
		t.Fatalf("State = %v, want closed", b.State())
	}
	b.Do(func() error { return fmt.Errorf("call: %w", errDown) })
	if b.State() != Open {
		t.Errorf("третья неудача подряд (обёрнутая): State = %v, want open", b.State())
	}
}

func TestHiddenTimeoutBoundaryAndLazyTransition(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 2, OpenTimeout: 3 * time.Second, HalfOpenProbes: 1, Now: clk.Now})
	b.Do(fail)
	clk.Advance(10 * time.Second) // в closed время ничего не значит
	b.Do(fail)
	if b.State() != Open {
		t.Fatalf("State = %v, want open", b.State())
	}
	clk.Advance(3*time.Second - time.Nanosecond)
	fn, calls := counting(nil)
	if err := b.Do(fn); err != ErrOpen || *calls != 0 {
		t.Fatalf("за 1 нс до таймаута: Do = %v, вызовов %d; want ErrOpen и 0", err, *calls)
	}
	clk.Advance(time.Nanosecond)
	// Переход в half-open происходит и через Do, без вызова State.
	if err := b.Do(fn); err != nil || *calls != 1 {
		t.Fatalf("ровно OpenTimeout: Do = %v, вызовов %d; want nil и 1", err, *calls)
	}
	if b.State() != Closed {
		t.Errorf("State = %v, want closed", b.State())
	}
}

func TestHiddenOpenedAtIsResultTime(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 1, OpenTimeout: 5 * time.Second, HalfOpenProbes: 1, Now: clk.Now})
	// Неудача, которая длилась 4 с: таймаут отсчитывается от её конца.
	c := start(t, b)
	clk.Advance(4 * time.Second)
	c.finish(t, errDown)
	clk.Advance(4 * time.Second)
	if b.State() != Open {
		t.Errorf("через 4 с после неудачи State = %v, want open", b.State())
	}
	clk.Advance(time.Second)
	if b.State() != HalfOpen {
		t.Errorf("через 5 с после неудачи State = %v, want half-open", b.State())
	}
}

func TestHiddenHalfOpenLimitsProbes(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 1, OpenTimeout: time.Second, HalfOpenProbes: 2, Now: clk.Now})
	b.Do(fail)
	clk.Advance(time.Second)

	p1 := start(t, b)
	p2 := start(t, b)
	fn, calls := counting(nil)
	if err := b.Do(fn); err != ErrOpen || *calls != 0 {
		t.Fatalf("третья проба при лимите 2: Do = %v, вызовов %d; want ErrOpen и 0", err, *calls)
	}
	if err := p1.finish(t, nil); err != nil {
		t.Fatalf("проба 1: %v", err)
	}
	if b.State() != HalfOpen {
		t.Fatalf("1 удачная проба из 2: State = %v, want half-open", b.State())
	}
	// Проба 1 завершилась — место освободилось.
	p3 := start(t, b)
	if err := p2.finish(t, nil); err != nil {
		t.Fatalf("проба 2: %v", err)
	}
	if b.State() != Closed {
		t.Fatalf("2 удачные пробы: State = %v, want closed", b.State())
	}
	// p3 начиналась в half-open: её неудача после замыкания устарела.
	if err := p3.finish(t, errDown); err != errDown {
		t.Fatalf("p3: Do = %v, want %v", err, errDown)
	}
	b.Do(fail) // при пороге 1 эта неудача — первая в closed
	if b.State() != Open {
		t.Fatalf("State = %v, want open (порог 1)", b.State())
	}
}

func TestHiddenHalfOpenFailureDropsOtherProbes(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 1, OpenTimeout: time.Second, HalfOpenProbes: 2, Now: clk.Now})
	b.Do(fail)
	clk.Advance(time.Second)
	p1 := start(t, b)
	p2 := start(t, b)
	p1.finish(t, errDown)
	if b.State() != Open {
		t.Fatalf("неудачная проба: State = %v, want open", b.State())
	}
	// Удача второй пробы пришла после размыкания — не замыкает breaker.
	if err := p2.finish(t, nil); err != nil {
		t.Fatalf("p2: Do = %v, want nil", err)
	}
	if b.State() != Open {
		t.Errorf("устаревшая удача: State = %v, want open", b.State())
	}
}

func TestHiddenStaleResultFromClosed(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 2, OpenTimeout: time.Second, HalfOpenProbes: 1, Now: clk.Now})
	slowOK := start(t, b) // начался в closed
	slowFail := start(t, b)
	b.Do(fail)
	b.Do(fail)
	if b.State() != Open {
		t.Fatalf("State = %v, want open", b.State())
	}
	slowOK.finish(t, nil)
	slowFail.finish(t, errDown)
	if b.State() != Open {
		t.Fatalf("итоги вызовов из closed не должны влиять на open: State = %v", b.State())
	}
	clk.Advance(time.Second)
	// Таймаут истёк: удачная проба замыкает breaker.
	if b.State() != HalfOpen {
		t.Fatalf("State = %v, want half-open", b.State())
	}
	probe := start(t, b)
	probe.finish(t, nil)
	if b.State() != Closed {
		t.Fatalf("State = %v, want closed", b.State())
	}
	// Ни одной неудачи из прошлых поколений не накопилось.
	b.Do(fail)
	if b.State() != Closed {
		t.Errorf("одна неудача при пороге 2: State = %v, want closed", b.State())
	}
}

func TestHiddenNoLockDuringCall(t *testing.T) {
	b := New(Config{FailureThreshold: 3, OpenTimeout: time.Second, HalfOpenProbes: 1, Now: newClock().Now})
	first := start(t, b)
	// Пока первый вызов идёт, второй и State не должны ждать.
	done := make(chan error, 1)
	go func() {
		b.State()
		done <- b.Do(ok)
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Errorf("второй вызов: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("второй вызов ждёт первый: Do держит блокировку во время fn")
	}
	first.finish(t, nil)
}

func TestHiddenConcurrentStress(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 5, OpenTimeout: 10 * time.Millisecond, HalfOpenProbes: 2, Now: clk.Now})
	var wg sync.WaitGroup
	var mu sync.Mutex
	calls := 0
	for g := range 16 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 300 {
				if i%50 == 0 {
					clk.Advance(5 * time.Millisecond)
				}
				err := b.Do(func() error {
					mu.Lock()
					calls++
					mu.Unlock()
					if (g+i)%3 == 0 {
						return errDown
					}
					return nil
				})
				if err != nil && err != errDown && err != ErrOpen {
					t.Errorf("Do = %v", err)
					return
				}
				if s := b.State(); s != Closed && s != Open && s != HalfOpen {
					t.Errorf("State = %d", s)
					return
				}
			}
		}()
	}
	wg.Wait()
	if calls == 0 {
		t.Error("fn не вызвана ни разу")
	}
}
