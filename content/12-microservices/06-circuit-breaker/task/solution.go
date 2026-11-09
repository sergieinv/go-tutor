package task

import (
	"errors"
	"sync"
	"time"
)

// State — состояние circuit breaker.
type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Closed:
		return "closed"
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	}
	return "unknown"
}

// ErrOpen — отказ без вызова: breaker разомкнут или пробы заняты.
var ErrOpen = errors.New("circuit breaker is open")

// Config — пороги и внедряемые зависимости.
type Config struct {
	FailureThreshold int
	OpenTimeout      time.Duration
	HalfOpenProbes   int
	IsFailure        func(error) bool
	Now              func() time.Time
}

// Breaker — circuit breaker по числу неудач подряд.
type Breaker struct {
	mu        sync.Mutex
	threshold int
	timeout   time.Duration
	probes    int
	isFailure func(error) bool
	now       func() time.Time

	state     State
	gen       uint64 // растёт при каждой смене состояния
	failures  int    // неудач подряд в Closed
	inFlight  int    // проб в полёте в HalfOpen
	successes int    // удачных проб в HalfOpen
	openedAt  time.Time
}

func New(cfg Config) *Breaker {
	b := &Breaker{
		threshold: max(cfg.FailureThreshold, 1),
		timeout:   cfg.OpenTimeout,
		probes:    max(cfg.HalfOpenProbes, 1),
		isFailure: cfg.IsFailure,
		now:       cfg.Now,
	}
	if b.isFailure == nil {
		b.isFailure = func(err error) bool { return err != nil }
	}
	if b.now == nil {
		b.now = time.Now
	}
	return b
}

// setState меняет состояние и начинает новое поколение. Вызывать под mu.
func (b *Breaker) setState(s State) {
	b.state = s
	b.gen++
	b.failures, b.inFlight, b.successes = 0, 0, 0
	if s == Open {
		b.openedAt = b.now()
	}
}

// refresh переводит Open в HalfOpen, когда таймаут истёк. Вызывать под mu.
func (b *Breaker) refresh() {
	if b.state == Open && b.now().Sub(b.openedAt) >= b.timeout {
		b.setState(HalfOpen)
	}
}

func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	return b.state
}

func (b *Breaker) Do(fn func() error) error {
	gen, err := b.before()
	if err != nil {
		return err
	}
	// fn выполняется без блокировки: одновременные вызовы не ждут друг друга.
	res := fn()
	b.after(gen, res)
	return res
}

func (b *Breaker) before() (uint64, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.refresh()
	switch b.state {
	case Open:
		return 0, ErrOpen
	case HalfOpen:
		if b.inFlight >= b.probes {
			return 0, ErrOpen
		}
		b.inFlight++
	}
	return b.gen, nil
}

func (b *Breaker) after(gen uint64, res error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if gen != b.gen {
		return // состояние сменилось, пока шёл вызов: итог устарел
	}
	failed := b.isFailure(res)
	switch b.state {
	case Closed:
		if !failed {
			b.failures = 0
			return
		}
		b.failures++
		if b.failures >= b.threshold {
			b.setState(Open)
		}
	case HalfOpen:
		b.inFlight--
		if failed {
			b.setState(Open)
			return
		}
		b.successes++
		if b.successes >= b.probes {
			b.setState(Closed)
		}
	}
}
