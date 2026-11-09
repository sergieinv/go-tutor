package task

import (
	"container/list"
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrOverloaded   = errors.New("overloaded: queue is full")
	ErrQueueTimeout = errors.New("overloaded: queue wait timeout")
)

type Config struct {
	MaxConcurrent int
	MaxQueue      int
	QueueTimeout  time.Duration
	After         func(time.Duration) <-chan time.Time
}

type Stats struct {
	InFlight int
	Queued   int
	Accepted int64
	Rejected int64
	TimedOut int64
	Canceled int64
}

// waiter — вызов в очереди; ready закрывается, когда ему передали место.
type waiter struct {
	ready chan struct{}
}

type Limiter struct {
	cfg   Config
	mu    sync.Mutex
	queue list.List // *waiter, от старых к новым
	stats Stats
}

func NewLimiter(cfg Config) *Limiter {
	cfg.MaxConcurrent = max(cfg.MaxConcurrent, 1)
	cfg.MaxQueue = max(cfg.MaxQueue, 0)
	if cfg.After == nil {
		cfg.After = time.After
	}
	return &Limiter{cfg: cfg}
}

func (l *Limiter) Do(ctx context.Context, fn func(context.Context) error) error {
	l.mu.Lock()
	if err := ctx.Err(); err != nil {
		l.stats.Canceled++
		l.mu.Unlock()
		return err
	}
	// Есть место и никто не ждёт — выполняем сразу.
	if l.stats.InFlight < l.cfg.MaxConcurrent && l.queue.Len() == 0 {
		l.stats.InFlight++
		l.stats.Accepted++
		l.mu.Unlock()
		return l.run(ctx, fn)
	}
	// Очередь полна — отказ сразу, без ожидания.
	if l.queue.Len() >= l.cfg.MaxQueue {
		l.stats.Rejected++
		l.mu.Unlock()
		return ErrOverloaded
	}
	w := &waiter{ready: make(chan struct{})}
	elem := l.queue.PushBack(w)
	l.stats.Queued++
	l.mu.Unlock()

	timeout := l.cfg.After(l.cfg.QueueTimeout)
	select {
	case <-w.ready:
		return l.run(ctx, fn)
	case <-timeout:
		if l.leave(elem, w, &l.stats.TimedOut) {
			return ErrQueueTimeout
		}
	case <-ctx.Done():
		if l.leave(elem, w, &l.stats.Canceled) {
			return ctx.Err()
		}
	}
	// Место передали одновременно с таймаутом или отменой: оно уже наше.
	return l.run(ctx, fn)
}

// leave убирает вызов из очереди. false — место ему уже передали.
func (l *Limiter) leave(elem *list.Element, w *waiter, counter *int64) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	select {
	case <-w.ready:
		return false
	default:
	}
	l.queue.Remove(elem)
	l.stats.Queued--
	*counter++
	return true
}

// run выполняет fn и передаёт место старейшему в очереди или освобождает его.
func (l *Limiter) run(ctx context.Context, fn func(context.Context) error) error {
	defer func() {
		l.mu.Lock()
		defer l.mu.Unlock()
		if front := l.queue.Front(); front != nil {
			l.queue.Remove(front)
			l.stats.Queued--
			l.stats.Accepted++
			close(front.Value.(*waiter).ready) // InFlight не меняется: место переходит
			return
		}
		l.stats.InFlight--
	}()
	return fn(ctx)
}

func (l *Limiter) Stats() Stats {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.stats
}
