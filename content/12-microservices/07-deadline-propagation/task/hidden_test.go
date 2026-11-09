package task

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type ctxKey struct{}

func TestHiddenChildExactlyMin(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), now.Add(300*time.Millisecond))
	defer cancel()
	// 300 − 100 = 200 мс — ровно Min: звать можно.
	child, stop, err := budget(now).Child(parent)
	if err != nil {
		t.Fatalf("остаток ровно Min: Child = %v, want nil", err)
	}
	defer stop()
	if got := deadlineOf(t, child).Sub(now); got != 200*time.Millisecond {
		t.Errorf("дедлайн now+%v, want now+200ms", got)
	}
	// На наносекунду меньше — уже нет.
	parent2, cancel2 := context.WithDeadline(context.Background(), now.Add(300*time.Millisecond-time.Nanosecond))
	defer cancel2()
	if _, _, err := budget(now).Child(parent2); !errors.Is(err, ErrNoTime) {
		t.Errorf("на 1 нс меньше Min: Child = %v, want ErrNoTime", err)
	}
}

func TestHiddenChildReserveLargerThanLeft(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), now.Add(50*time.Millisecond))
	defer cancel()
	b := Budget{Reserve: 100 * time.Millisecond, Max: time.Second, Min: 0, Now: fixed(now)}
	// Остаток минус запас отрицателен — меньше Min даже при Min = 0.
	if c, s, err := b.Child(parent); !errors.Is(err, ErrNoTime) || c != nil || s != nil {
		t.Errorf("Child = (%v, %v), want (nil, nil, ErrNoTime)", c, err)
	}
}

func TestHiddenChildParentDone(t *testing.T) {
	now := time.Now()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if c, s, err := budget(now).Child(ctx); !errors.Is(err, context.Canceled) || c != nil || s != nil {
		t.Errorf("отменённый родитель: Child = (%v, %v), want (nil, nil, context.Canceled)", c, err)
	}
	// Дедлайн истёк по-настоящему, хотя внедрённые часы «отстают»:
	// ошибка контекста важнее арифметики.
	expired, cancel2 := context.WithDeadline(context.Background(), now.Add(-time.Second))
	defer cancel2()
	b := budget(now.Add(-time.Minute))
	if c, s, err := b.Child(expired); !errors.Is(err, context.DeadlineExceeded) || c != nil || s != nil {
		t.Errorf("истёкший родитель: Child = (%v, %v), want (nil, nil, context.DeadlineExceeded)", c, err)
	}
}

func TestHiddenChildInheritsCancelAndValues(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithCancel(context.WithValue(context.Background(), ctxKey{}, "req-42"))
	child, stop, err := budget(now).Child(parent)
	if err != nil {
		t.Fatalf("Child: %v", err)
	}
	defer stop()
	if v := child.Value(ctxKey{}); v != "req-42" {
		t.Errorf("значение контекста потерялось: %v", v)
	}
	cancel()
	select {
	case <-child.Done():
		if !errors.Is(child.Err(), context.Canceled) {
			t.Errorf("child.Err() = %v, want context.Canceled", child.Err())
		}
	case <-time.After(2 * time.Second):
		t.Fatal("отмена родителя не дошла до дочернего контекста")
	}
}

func TestHiddenChildCancelFunc(t *testing.T) {
	now := time.Now()
	child, stop, err := budget(now).Child(context.Background())
	if err != nil {
		t.Fatalf("Child: %v", err)
	}
	stop()
	if !errors.Is(child.Err(), context.Canceled) {
		t.Errorf("после cancel: child.Err() = %v, want context.Canceled", child.Err())
	}
}

func TestHiddenChildRealClock(t *testing.T) {
	before := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), before.Add(10*time.Second))
	defer cancel()
	b := Budget{Reserve: time.Second, Max: time.Hour, Min: time.Millisecond}
	child, stop, err := b.Child(parent)
	if err != nil {
		t.Fatalf("Now == nil: Child = %v", err)
	}
	defer stop()
	dl := deadlineOf(t, child)
	// Дедлайн — дедлайн родителя минус запас (по настоящим часам он не позже).
	if dl.After(before.Add(9*time.Second)) || dl.Before(before.Add(8*time.Second)) {
		t.Errorf("Now == nil: дедлайн now+%v, want ≈ now+9s", dl.Sub(before))
	}
}

func TestHiddenGatherNoCalls(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := Gather(ctx, budget(time.Now())); err != nil {
		t.Errorf("Gather без вызовов = %v, want nil", err)
	}
}

func TestHiddenGatherParentCancelReachesAll(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithCancel(context.Background())
	var started sync.WaitGroup
	started.Add(3)
	var saw atomic.Int32
	call := func(ctx context.Context) error {
		started.Done()
		select {
		case <-ctx.Done():
			saw.Add(1)
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return errors.New("отмена не дошла")
		}
	}
	go func() {
		started.Wait() // все трое уже ждут
		cancel()
	}()
	done := make(chan error, 1)
	go func() { done <- Gather(parent, budget(now), call, call, call) }()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Gather = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		cancel()
		t.Fatal("Gather не вернулся за 5 с: вызовы идут не параллельно или отмена не дошла")
	}
	if saw.Load() != 3 {
		t.Errorf("отмену увидели %d из 3", saw.Load())
	}
}

func TestHiddenGatherWaitsForAll(t *testing.T) {
	now := time.Now()
	boom := errors.New("boom")
	var finished atomic.Int32
	release := make(chan struct{})
	ignoring := func(context.Context) error {
		<-release // не смотрит на ctx — Gather всё равно должен его дождаться
		finished.Add(1)
		return nil
	}
	failing := func(context.Context) error {
		close(release)
		return boom
	}
	err := Gather(context.Background(), budget(now), ignoring, ignoring, failing)
	if err != boom {
		t.Errorf("Gather = %v, want %v", err, boom)
	}
	if finished.Load() != 2 {
		t.Errorf("Gather вернулся, когда завершилось %d из 2 вызовов", finished.Load())
	}
}

func TestHiddenGatherRunsConcurrently(t *testing.T) {
	now := time.Now()
	const n = 4
	var started sync.WaitGroup
	started.Add(n)
	all := make(chan struct{})
	go func() { started.Wait(); close(all) }()
	call := func(context.Context) error {
		started.Done()
		select {
		case <-all:
			return nil
		case <-time.After(3 * time.Second):
			return errors.New("остальные вызовы не начались — Gather вызывает по очереди")
		}
	}
	calls := make([]func(context.Context) error, n)
	for i := range calls {
		calls[i] = call
	}
	if err := Gather(context.Background(), budget(now), calls...); err != nil {
		t.Error(err)
	}
}

func TestHiddenGatherCancelsChildAfterReturn(t *testing.T) {
	now := time.Now()
	var got []context.Context
	var mu sync.Mutex
	call := func(ctx context.Context) error {
		mu.Lock()
		got = append(got, ctx)
		mu.Unlock()
		return nil
	}
	if err := Gather(context.Background(), budget(now), call, call); err != nil {
		t.Fatalf("Gather = %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("вызовов %d, want 2", len(got))
	}
	for _, ctx := range got {
		if ctx.Err() == nil {
			t.Error("контекст вызова не отменён после возврата Gather — таймер и горутины контекста утекут")
		}
		if v, ok := ctx.Deadline(); !ok || !v.Equal(now.Add(2*time.Second)) {
			t.Errorf("дедлайн вызова now+%v, want now+2s (Max, у родителя дедлайна нет)", v.Sub(now))
		}
	}
}

func TestHiddenGatherKeepsValues(t *testing.T) {
	ctx := context.WithValue(context.Background(), ctxKey{}, "req-7")
	var v atomic.Value
	err := Gather(ctx, budget(time.Now()), func(ctx context.Context) error {
		v.Store(ctx.Value(ctxKey{}))
		return nil
	})
	if err != nil || v.Load() != "req-7" {
		t.Errorf("Gather = %v, значение в вызове %v; want nil и req-7 (контекст — потомок входящего)", err, v.Load())
	}
}
