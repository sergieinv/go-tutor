package task

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"
)

// fixed — часы, которые всегда показывают t. Начинаются с настоящего
// времени, чтобы дедлайны контекстов были в будущем.
func fixed(t time.Time) func() time.Time { return func() time.Time { return t } }

func budget(now time.Time) Budget {
	return Budget{Reserve: 100 * time.Millisecond, Max: 2 * time.Second, Min: 200 * time.Millisecond, Now: fixed(now)}
}

// deadlineOf возвращает дедлайн контекста или проваливает тест.
func deadlineOf(t *testing.T, ctx context.Context) time.Time {
	t.Helper()
	if ctx == nil {
		t.Fatal("контекст nil")
	}
	dl, ok := ctx.Deadline()
	if !ok {
		t.Fatal("у дочернего контекста нет дедлайна")
	}
	return dl
}

func TestChildSubtractsReserve(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), now.Add(time.Second))
	defer cancel()
	child, stop, err := budget(now).Child(parent)
	if err != nil {
		t.Fatalf("Child: %v", err)
	}
	defer stop()
	if got, want := deadlineOf(t, child), now.Add(900*time.Millisecond); !got.Equal(want) {
		t.Errorf("дедлайн = now+%v, want now+900ms", got.Sub(now))
	}
}

func TestChildCappedByMax(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), now.Add(time.Minute))
	defer cancel()
	child, stop, err := budget(now).Child(parent)
	if err != nil {
		t.Fatalf("Child: %v", err)
	}
	defer stop()
	if got := deadlineOf(t, child).Sub(now); got != 2*time.Second {
		t.Errorf("дедлайн = now+%v, want now+2s (Max)", got)
	}
}

func TestChildWithoutDeadlineUsesMax(t *testing.T) {
	now := time.Now()
	child, stop, err := budget(now).Child(context.Background())
	if err != nil {
		t.Fatalf("Child: %v", err)
	}
	defer stop()
	if got := deadlineOf(t, child).Sub(now); got != 2*time.Second {
		t.Errorf("без дедлайна: now+%v, want now+2s", got)
	}
}

func TestChildNoTime(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), now.Add(250*time.Millisecond))
	defer cancel()
	// 250 − 100 = 150 мс < Min 200 мс.
	child, stop, err := budget(now).Child(parent)
	if !errors.Is(err, ErrNoTime) {
		t.Fatalf("Child = %v, want ErrNoTime", err)
	}
	if child != nil || stop != nil {
		t.Error("при ошибке контекст и cancel должны быть nil")
	}
}

func TestGatherAllSucceed(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), now.Add(time.Second))
	defer cancel()
	var calls atomic.Int32
	call := func(ctx context.Context) error {
		calls.Add(1)
		if dl, ok := ctx.Deadline(); !ok || !dl.Equal(now.Add(900*time.Millisecond)) {
			t.Errorf("вызов получил дедлайн now+%v (есть: %v), want now+900ms", dl.Sub(now), ok)
		}
		return nil
	}
	if err := Gather(parent, budget(now), call, call, call); err != nil {
		t.Fatalf("Gather = %v, want nil", err)
	}
	if calls.Load() != 3 {
		t.Errorf("вызовов %d, want 3", calls.Load())
	}
}

func TestGatherFirstErrorCancelsOthers(t *testing.T) {
	now := time.Now()
	boom := errors.New("warehouse: out of stock")
	var canceled atomic.Int32
	slow := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			canceled.Add(1)
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return errors.New("отмена не дошла")
		}
	}
	failing := func(context.Context) error { return boom }
	err := Gather(context.Background(), budget(now), slow, failing, slow)
	if err != boom {
		t.Fatalf("Gather = %v, want %v", err, boom)
	}
	if canceled.Load() != 2 {
		t.Errorf("отмену увидели %d из 2 медленных вызовов", canceled.Load())
	}
}

func TestGatherNoTimeSkipsCalls(t *testing.T) {
	now := time.Now()
	parent, cancel := context.WithDeadline(context.Background(), now.Add(150*time.Millisecond))
	defer cancel()
	called := false
	err := Gather(parent, budget(now), func(context.Context) error { called = true; return nil })
	if !errors.Is(err, ErrNoTime) || called {
		t.Errorf("Gather = %v, вызов был: %v; want ErrNoTime без вызова", err, called)
	}
}
