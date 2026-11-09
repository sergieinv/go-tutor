package task

import (
	"errors"
	"sync"
	"testing"
	"time"
)

// clock — управляемые часы, безопасные для горутин.
type clock struct {
	mu sync.Mutex
	t  time.Time
}

func newClock() *clock { return &clock{t: time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

var (
	errDown = errors.New("unavailable")
	errBad  = errors.New("invalid argument")
)

func fail() error { return errDown }
func ok() error   { return nil }

// counting возвращает fn и счётчик её вызовов.
func counting(res error) (func() error, *int) {
	n := 0
	return func() error { n++; return res }, &n
}

func TestStateString(t *testing.T) {
	for s, want := range map[State]string{Closed: "closed", Open: "open", HalfOpen: "half-open"} {
		if got := s.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestOpensAfterThreshold(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 3, OpenTimeout: 5 * time.Second, HalfOpenProbes: 1, Now: clk.Now})
	for i := range 2 {
		if err := b.Do(fail); err != errDown {
			t.Fatalf("вызов %d: Do = %v, want %v", i+1, err, errDown)
		}
	}
	if b.State() != Closed {
		t.Fatalf("после 2 неудач State = %v, want closed", b.State())
	}
	b.Do(fail)
	if b.State() != Open {
		t.Fatalf("после 3 неудач подряд State = %v, want open", b.State())
	}
	fn, calls := counting(nil)
	if err := b.Do(fn); err != ErrOpen {
		t.Errorf("Do в open = %v, want ErrOpen", err)
	}
	if *calls != 0 {
		t.Errorf("в open fn вызвана %d раз, want 0", *calls)
	}
}

func TestSuccessResetsCounter(t *testing.T) {
	b := New(Config{FailureThreshold: 3, OpenTimeout: time.Second, HalfOpenProbes: 1, Now: newClock().Now})
	b.Do(fail)
	b.Do(fail)
	b.Do(ok)
	b.Do(fail)
	b.Do(fail)
	if b.State() != Closed {
		t.Errorf("неудачи не подряд: State = %v, want closed", b.State())
	}
}

func TestIsFailureFilters(t *testing.T) {
	isFailure := func(err error) bool { return errors.Is(err, errDown) }
	b := New(Config{FailureThreshold: 2, OpenTimeout: time.Second, HalfOpenProbes: 1, IsFailure: isFailure, Now: newClock().Now})
	for range 5 {
		if err := b.Do(func() error { return errBad }); err != errBad {
			t.Fatalf("Do = %v, want ошибку fn как есть", err)
		}
	}
	if b.State() != Closed {
		t.Errorf("errBad — не неудача: State = %v, want closed", b.State())
	}
}

func TestHalfOpenProbeCloses(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 1, OpenTimeout: 5 * time.Second, HalfOpenProbes: 1, Now: clk.Now})
	b.Do(fail)
	clk.Advance(4 * time.Second)
	if b.State() != Open {
		t.Fatalf("через 4 с State = %v, want open", b.State())
	}
	clk.Advance(time.Second)
	if b.State() != HalfOpen {
		t.Fatalf("через 5 с State = %v, want half-open", b.State())
	}
	if err := b.Do(ok); err != nil {
		t.Fatalf("проба: Do = %v, want nil", err)
	}
	if b.State() != Closed {
		t.Errorf("после удачной пробы State = %v, want closed", b.State())
	}
}

func TestHalfOpenProbeFailureReopens(t *testing.T) {
	clk := newClock()
	b := New(Config{FailureThreshold: 1, OpenTimeout: 5 * time.Second, HalfOpenProbes: 1, Now: clk.Now})
	b.Do(fail)
	clk.Advance(5 * time.Second)
	if err := b.Do(fail); err != errDown {
		t.Fatalf("проба: Do = %v, want %v", err, errDown)
	}
	if b.State() != Open {
		t.Fatalf("после неудачной пробы State = %v, want open", b.State())
	}
	clk.Advance(4 * time.Second)
	if b.State() != Open {
		t.Errorf("таймаут отсчитывается заново: через 4 с State = %v, want open", b.State())
	}
}
