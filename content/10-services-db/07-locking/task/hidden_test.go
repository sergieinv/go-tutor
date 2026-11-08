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

func TestGetNotFound(t *testing.T) {
	s, err := Get(context.Background(), newDB(t), 42)
	if !errors.Is(err, ErrNotFound) || s != (Stock{}) {
		t.Fatalf("Get(42) = %+v, %v, want нулевой Stock и ErrNotFound", s, err)
	}
}

func TestSaveMissingRow(t *testing.T) {
	err := Save(context.Background(), newDB(t), Stock{ID: 42, Qty: 1, Version: 1})
	if !errors.Is(err, ErrConflict) {
		t.Fatalf("Save несуществующей строки: err = %v, want ErrConflict", err)
	}
}

func TestSaveKeepsOtherRows(t *testing.T) {
	db := newDB(t)
	if err := Save(context.Background(), db, Stock{ID: 2, Qty: 5, Version: 7}); err != nil {
		t.Fatalf("Save({2 5 7}): %v", err)
	}
	if qty, ver := row(t, db, 2); qty != 5 || ver != 8 {
		t.Fatalf("строка 2: qty %d, version %d, want 5, 8", qty, ver)
	}
	if qty, ver := row(t, db, 1); qty != 10 || ver != 1 {
		t.Fatalf("строка 1 изменилась: qty %d, version %d, want 10, 1", qty, ver)
	}
}

func TestTryReserveErrors(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	for _, n := range []int64{0, -3} {
		if err := TryReserve(ctx, db, 1, n); !errors.Is(err, ErrInvalid) {
			t.Errorf("TryReserve(1, %d): err = %v, want ErrInvalid", n, err)
		}
	}
	if err := TryReserve(ctx, db, 42, 1); !errors.Is(err, ErrNotFound) {
		t.Errorf("TryReserve(42, 1): err = %v, want ErrNotFound", err)
	}
	if err := TryReserve(ctx, db, 2, 1); !errors.Is(err, ErrInsufficient) {
		t.Errorf("TryReserve(2, 1) при нулевом остатке: err = %v, want ErrInsufficient", err)
	}
	if err := TryReserve(ctx, db, 1, 11); !errors.Is(err, ErrInsufficient) {
		t.Errorf("TryReserve(1, 11) при остатке 10: err = %v, want ErrInsufficient", err)
	}
	if qty, ver := row(t, db, 1); qty != 10 || ver != 1 {
		t.Errorf("после ошибок строка 1: qty %d, version %d, want 10, 1", qty, ver)
	}
	if err := TryReserve(ctx, db, 1, 10); err != nil {
		t.Errorf("TryReserve(1, 10) при остатке 10: %v", err)
	}
	if qty, ver := row(t, db, 1); qty != 0 || ver != 2 {
		t.Errorf("после списания всего: qty %d, version %d, want 0, 2", qty, ver)
	}
}

func TestRetryGivesUp(t *testing.T) {
	s := &noSleep{}
	var backoffs []int
	calls := 0
	r := Retry{
		Attempts: 4,
		Backoff:  func(a int) time.Duration { backoffs = append(backoffs, a); return time.Duration(a) * time.Millisecond },
		Sleep:    s.Sleep,
	}
	err := r.Do(context.Background(), func(ctx context.Context) error { calls++; return ErrConflict })
	if !errors.Is(err, ErrConflict) || calls != 4 {
		t.Fatalf("Do при постоянном конфликте: err = %v, вызовов %d, want ErrConflict и 4", err, calls)
	}
	if len(backoffs) != 3 || backoffs[0] != 1 || backoffs[1] != 2 || backoffs[2] != 3 {
		t.Errorf("Backoff вызван с %v, want [1 2 3] — после последней попытки пауза не нужна", backoffs)
	}
	if len(s.got) != 3 || s.got[2] != 3*time.Millisecond {
		t.Errorf("паузы %v, want [1ms 2ms 3ms]", s.got)
	}
}

func TestRetryOtherErrors(t *testing.T) {
	errBoom := errors.New("boom")
	for _, want := range []error{errBoom, ErrInsufficient, ErrNotFound} {
		s := &noSleep{}
		calls := 0
		r := Retry{Attempts: 5, Sleep: s.Sleep}
		err := r.Do(context.Background(), func(ctx context.Context) error {
			calls++
			return errors.Join(errors.New("обёртка"), want)
		})
		if !errors.Is(err, want) || calls != 1 || len(s.got) != 0 {
			t.Errorf("Do с ошибкой %v: err = %v, вызовов %d, пауз %d; want ту же ошибку, 1 вызов, 0 пауз", want, err, calls, len(s.got))
		}
	}
}

func TestRetryWrappedConflict(t *testing.T) {
	calls := 0
	r := Retry{Attempts: 3, Sleep: (&noSleep{}).Sleep}
	r.Do(context.Background(), func(ctx context.Context) error {
		calls++
		return errors.Join(errors.New("save"), ErrConflict)
	})
	if calls != 3 {
		t.Fatalf("обёрнутый ErrConflict: вызовов %d, want 3 — сравнивайте через errors.Is", calls)
	}
}

func TestRetryAttemptsBelowOne(t *testing.T) {
	for _, attempts := range []int{0, -1} {
		calls := 0
		r := Retry{Attempts: attempts, Sleep: (&noSleep{}).Sleep}
		err := r.Do(context.Background(), func(ctx context.Context) error { calls++; return ErrConflict })
		if calls != 1 || !errors.Is(err, ErrConflict) {
			t.Errorf("Attempts = %d: вызовов %d, err = %v, want 1 и ErrConflict", attempts, calls, err)
		}
	}
}

func TestRetryNilBackoff(t *testing.T) {
	s := &noSleep{}
	r := Retry{Attempts: 3, Sleep: s.Sleep}
	r.Do(context.Background(), func(ctx context.Context) error { return ErrConflict })
	if len(s.got) != 2 || s.got[0] != 0 || s.got[1] != 0 {
		t.Fatalf("без Backoff паузы %v, want [0s 0s]", s.got)
	}
}

func TestRetryContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	r := Retry{Attempts: 5, Sleep: (&noSleep{}).Sleep}
	err := r.Do(ctx, func(ctx context.Context) error {
		calls++
		cancel() // отмена во время первой попытки: Sleep вернёт ctx.Err()
		return ErrConflict
	})
	if !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("отмена во время попытки: err = %v, вызовов %d, want context.Canceled и 1", err, calls)
	}

	calls = 0
	err = r.Do(ctx, func(ctx context.Context) error { calls++; return nil })
	if !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatalf("уже отменённый контекст: err = %v, вызовов %d, want context.Canceled и 0", err, calls)
	}
}

func TestRetryPassesContext(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "v")
	r := Retry{Attempts: 1}
	var got any
	r.Do(ctx, func(ctx context.Context) error { got = ctx.Value(key{}); return nil })
	if got != "v" {
		t.Fatalf("fn получила контекст без значения вызывающего")
	}
}

func TestRetryDefaultSleep(t *testing.T) {
	// Sleep не задан — настоящее ожидание, но прерываемое контекстом.
	ctx, cancel := context.WithCancel(context.Background())
	r := Retry{Attempts: 2, Backoff: func(int) time.Duration { return time.Hour }}
	done := make(chan error, 1)
	go func() {
		done <- r.Do(ctx, func(ctx context.Context) error { return ErrConflict })
	}()
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Do = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Do без Sleep не прервалась отменой контекста за 5 с")
	}
}

func TestExpBackoffBounds(t *testing.T) {
	zero := ExpBackoff(10*time.Millisecond, time.Second, func() float64 { return 0 })
	if got := zero(3); got != 0 {
		t.Errorf("rnd = 0: пауза %v, want 0", got)
	}
	almost := ExpBackoff(10*time.Millisecond, time.Second, func() float64 { return 0.999 })
	for _, attempt := range []int{1, 7, 8, 63, 64, 1000} {
		got := almost(attempt)
		if got < 0 || got >= time.Second {
			t.Errorf("ExpBackoff(10ms, 1s)(%d) = %v, want в [0, 1s)", attempt, got)
		}
	}
	if got := almost(1000); got < 990*time.Millisecond {
		t.Errorf("большой attempt: пауза %v, want около max (rnd = 0.999)", got)
	}
	half := ExpBackoff(10*time.Millisecond, time.Second, func() float64 { return 0.5 })
	if got := half(0); got != 5*time.Millisecond {
		t.Errorf("attempt 0 считается первой попыткой: %v, want 5ms", got)
	}
}

func TestConcurrentReserve(t *testing.T) {
	db := newDB(t)
	if _, err := db.Exec(`UPDATE stock SET qty = 15 WHERE id = 1`); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	yield := func(ctx context.Context, d time.Duration) error { runtime.Gosched(); return ctx.Err() }
	r := Retry{Attempts: 30, Sleep: yield}
	var ok, short atomic.Int64
	var wg sync.WaitGroup
	for range 20 {
		wg.Go(func() {
			switch err := Reserve(ctx, db, r, 1, 1); {
			case err == nil:
				ok.Add(1)
			case errors.Is(err, ErrInsufficient):
				short.Add(1)
			default:
				t.Errorf("Reserve: %v, want nil или ErrInsufficient", err)
			}
		})
	}
	wg.Wait()
	qty, ver := row(t, db, 1)
	if ok.Load() != 15 || short.Load() != 5 || qty != 0 || ver != 16 {
		t.Fatalf("успешно %d, не хватило %d, остаток %d, версия %d; want 15, 5, 0, 16 — обновления теряются или повторов мало", ok.Load(), short.Load(), qty, ver)
	}
}
