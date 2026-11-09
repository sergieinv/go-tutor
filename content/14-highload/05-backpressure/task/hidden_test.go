package task

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// Точное число отказов: N = 3, M = 4, приходят 20 вызовов, пока все
// принятые ещё работают.
func TestHiddenExactShedCount(t *testing.T) {
	fa := &fakeAfter{}
	l := NewLimiter(Config{MaxConcurrent: 3, MaxQueue: 4, QueueTimeout: time.Second, After: fa.After})
	var run []*call
	for range 3 {
		run = append(run, running(t, l))
	}
	var q []*call
	for i := range 4 {
		q = append(q, queued(t, l, context.Background(), i+1))
	}
	rejected := 0
	for range 13 {
		if err := l.Do(context.Background(), func(context.Context) error { return nil }); errors.Is(err, ErrOverloaded) {
			rejected++
		}
	}
	if rejected != 13 {
		t.Fatalf("отказов %d из 13, want 13", rejected)
	}
	if d := fa.calls(); len(d) != 4 {
		t.Errorf("After вызван %d раз, want 4 — по разу на вызов в очереди, не для принятых сразу и не для отказов", len(d))
	}
	want := Stats{InFlight: 3, Queued: 4, Accepted: 3, Rejected: 13}
	if s := l.Stats(); s != want {
		t.Fatalf("Stats = %+v, want %+v", s, want)
	}
	for _, c := range run {
		c.finish(t, nil)
	}
	for _, c := range q[:3] {
		c.begun(t)
	}
	q[0].finish(t, nil)
	q[3].begun(t)
	for _, c := range q[1:] {
		c.finish(t, nil)
	}
	want = Stats{Accepted: 7, Rejected: 13}
	if s := l.Stats(); s != want {
		t.Errorf("в конце Stats = %+v, want %+v", s, want)
	}
}

// Освободившееся место получает тот, кто ждёт дольше всех.
func TestHiddenFIFO(t *testing.T) {
	fa := &fakeAfter{}
	l := NewLimiter(Config{MaxConcurrent: 1, MaxQueue: 3, QueueTimeout: time.Second, After: fa.After})
	a := running(t, l)
	q := []*call{
		queued(t, l, context.Background(), 1),
		queued(t, l, context.Background(), 2),
		queued(t, l, context.Background(), 3),
	}
	a.finish(t, nil)
	for i, c := range q {
		c.begun(t)
		for _, other := range q[i+1:] {
			select {
			case <-other.started:
				t.Fatal("место получил не самый старый вызов в очереди")
			default:
			}
		}
		if s := l.Stats(); s.InFlight != 1 || s.Queued != len(q)-i-1 {
			t.Fatalf("после передачи места Stats = %+v, want InFlight 1, Queued %d", s, len(q)-i-1)
		}
		c.finish(t, nil)
	}
}

// Ушедший по таймауту освобождает место в очереди, а не занимает его навсегда.
func TestHiddenTimeoutFreesQueue(t *testing.T) {
	fa := &fakeAfter{}
	l := NewLimiter(Config{MaxConcurrent: 1, MaxQueue: 1, QueueTimeout: 30 * time.Millisecond, After: fa.After})
	a := running(t, l)
	q := queued(t, l, context.Background(), 1)
	fa.fire(t, 0)
	if err := q.wait(t); !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("Do = %v, want ErrQueueTimeout", err)
	}
	select {
	case <-q.started:
		t.Fatal("fn вызвана после таймаута очереди")
	default:
	}
	q2 := queued(t, l, context.Background(), 1)
	a.finish(t, nil)
	q2.begun(t)
	q2.finish(t, nil)
	want := Stats{Accepted: 2, TimedOut: 1}
	if s := l.Stats(); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
}

func TestHiddenCancelInQueue(t *testing.T) {
	fa := &fakeAfter{}
	l := NewLimiter(Config{MaxConcurrent: 1, MaxQueue: 2, QueueTimeout: time.Second, After: fa.After})
	a := running(t, l)
	ctx, cancel := context.WithCancel(context.Background())
	q := queued(t, l, ctx, 1)
	cancel()
	if err := q.wait(t); !errors.Is(err, context.Canceled) {
		t.Fatalf("Do после отмены в очереди = %v, want context.Canceled", err)
	}
	waitFor(t, l, "Queued == 0", func(s Stats) bool { return s.Queued == 0 })
	a.finish(t, nil)
	want := Stats{Accepted: 1, Canceled: 1}
	if s := l.Stats(); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
}

func TestHiddenCanceledBeforeCall(t *testing.T) {
	l := NewLimiter(Config{MaxConcurrent: 1, MaxQueue: 1, QueueTimeout: time.Second})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := l.Do(ctx, func(context.Context) error {
		t.Error("fn вызвана с уже отменённым контекстом")
		return nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Do = %v, want context.Canceled", err)
	}
	want := Stats{Canceled: 1}
	if s := l.Stats(); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
}

// fn получает контекст вызова, а место освобождается и после ошибки.
func TestHiddenContextAndErrorRelease(t *testing.T) {
	l := NewLimiter(Config{MaxConcurrent: 1, MaxQueue: 0, QueueTimeout: time.Second})
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "orders")
	boom := errors.New("db down")
	err := l.Do(ctx, func(c context.Context) error {
		if c.Value(key{}) != "orders" {
			t.Error("fn получила не контекст вызова")
		}
		return boom
	})
	if err != boom {
		t.Fatalf("Do = %v, want ошибку fn как есть", err)
	}
	if err := l.Do(ctx, func(context.Context) error { return nil }); err != nil {
		t.Fatalf("место не освободилось после ошибки fn: Do = %v", err)
	}
	want := Stats{Accepted: 2}
	if s := l.Stats(); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
}

// MaxConcurrent < 1 — как 1, MaxQueue < 0 — как 0 (очереди нет).
func TestHiddenDefaults(t *testing.T) {
	l := NewLimiter(Config{MaxConcurrent: 0, MaxQueue: -5, QueueTimeout: time.Second})
	a := running(t, l)
	err := l.Do(context.Background(), func(context.Context) error { return nil })
	if !errors.Is(err, ErrOverloaded) {
		t.Fatalf("MaxConcurrent 0 → 1, MaxQueue -5 → 0: второй Do = %v, want ErrOverloaded", err)
	}
	a.finish(t, nil)
	want := Stats{Accepted: 1, Rejected: 1}
	if s := l.Stats(); s != want {
		t.Errorf("Stats = %+v, want %+v", s, want)
	}
}

// After == nil — настоящий time.After.
func TestHiddenRealTimer(t *testing.T) {
	l := NewLimiter(Config{MaxConcurrent: 1, MaxQueue: 1, QueueTimeout: 20 * time.Millisecond})
	a := running(t, l)
	c := begin(l, context.Background())
	if err := c.wait(t); !errors.Is(err, ErrQueueTimeout) {
		t.Fatalf("Do = %v, want ErrQueueTimeout по настоящему таймеру", err)
	}
	a.finish(t, nil)
	if s := l.Stats(); s.TimedOut != 1 || s.InFlight != 0 || s.Queued != 0 {
		t.Errorf("Stats = %+v, want TimedOut 1, InFlight 0, Queued 0", s)
	}
}

// Под нагрузкой: fn никогда не выполняется больше чем у N горутин сразу,
// каждый вызов учтён ровно в одном счётчике, в конце всё освобождено.
func TestHiddenStress(t *testing.T) {
	const n, total = 4, 300
	l := NewLimiter(Config{MaxConcurrent: n, MaxQueue: 8, QueueTimeout: 5 * time.Second})
	var cur, peak, ran atomic.Int64
	var wg sync.WaitGroup
	for i := range total {
		ctx, cancel := context.WithCancel(context.Background())
		if i%10 == 0 {
			cancel() // часть вызовов отменена заранее
		}
		wg.Go(func() {
			defer cancel()
			l.Do(ctx, func(context.Context) error {
				c := cur.Add(1)
				for {
					p := peak.Load()
					if c <= p || peak.CompareAndSwap(p, c) {
						break
					}
				}
				ran.Add(1)
				time.Sleep(50 * time.Microsecond)
				cur.Add(-1)
				return nil
			})
		})
	}
	wg.Wait()
	s := l.Stats()
	if p := peak.Load(); p > n {
		t.Errorf("одновременно выполнялось %d вызовов fn, лимит %d", p, n)
	}
	if s.Accepted != ran.Load() {
		t.Errorf("Accepted = %d, а fn выполнилась %d раз", s.Accepted, ran.Load())
	}
	if sum := s.Accepted + s.Rejected + s.TimedOut + s.Canceled; sum != total {
		t.Errorf("Accepted+Rejected+TimedOut+Canceled = %d, want %d (Stats %+v)", sum, total, s)
	}
	if s.Canceled < total/10 {
		t.Errorf("Canceled = %d, want хотя бы %d (вызовы с отменённым контекстом)", s.Canceled, total/10)
	}
	if s.InFlight != 0 || s.Queued != 0 {
		t.Errorf("в конце InFlight = %d, Queued = %d, want 0 и 0", s.InFlight, s.Queued)
	}
}
