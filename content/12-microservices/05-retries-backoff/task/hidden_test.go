package task

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

func TestHiddenRetryableWrappedAndContext(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"обёрнутый Unavailable", fmt.Errorf("reserve: %w", status.Error(codes.Unavailable, "x")), true},
		{"дважды обёрнутый Aborted", fmt.Errorf("a: %w", fmt.Errorf("b: %w", status.Error(codes.Aborted, "x"))), true},
		{"обёрнутый InvalidArgument", fmt.Errorf("x: %w", status.Error(codes.InvalidArgument, "x")), false},
		{"context.Canceled", context.Canceled, false},
		{"context.DeadlineExceeded", context.DeadlineExceeded, false},
		{"Internal", status.Error(codes.Internal, "x"), false},
		{"Unknown", status.Error(codes.Unknown, "x"), false},
		{"OK-статус", status.New(codes.OK, "").Err(), false},
	}
	for _, tt := range tests {
		if got := Retryable(tt.err); got != tt.want {
			t.Errorf("%s: Retryable = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestHiddenRetryDelayEdgeCases(t *testing.T) {
	// Деталь RetryInfo после другой детали и в обёрнутой ошибке.
	st, err := status.New(codes.ResourceExhausted, "busy").WithDetails(
		&errdetails.ErrorInfo{Reason: "QUOTA"},
		&errdetails.RetryInfo{RetryDelay: durationpb.New(1500 * time.Millisecond)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := RetryDelay(fmt.Errorf("call: %w", st.Err())); !ok || d != 1500*time.Millisecond {
		t.Errorf("обёрнутая ошибка с двумя деталями: %v, %v; want 1.5s, true", d, ok)
	}
	if d, ok := RetryDelay(withRetryInfo(t, codes.Unavailable, durationpb.New(0))); !ok || d != 0 {
		t.Errorf("нулевая пауза: %v, %v; want 0, true", d, ok)
	}
	if d, ok := RetryDelay(withRetryInfo(t, codes.Unavailable, durationpb.New(-time.Second))); ok || d != 0 {
		t.Errorf("отрицательная пауза: %v, %v; want 0, false", d, ok)
	}
	if d, ok := RetryDelay(withRetryInfo(t, codes.Unavailable, nil)); ok || d != 0 {
		t.Errorf("RetryDelay не задан: %v, %v; want 0, false", d, ok)
	}
	if d, ok := RetryDelay(withRetryInfo(t, codes.Unavailable, &durationpb.Duration{Seconds: 1, Nanos: -5})); ok || d != 0 {
		t.Errorf("некорректная Duration: %v, %v; want 0, false", d, ok)
	}
	if d, ok := RetryDelay(nil); ok || d != 0 {
		t.Errorf("nil: %v, %v; want 0, false", d, ok)
	}
	if d, ok := RetryDelay(errors.New("plain")); ok || d != 0 {
		t.Errorf("не gRPC: %v, %v; want 0, false", d, ok)
	}
}

func TestHiddenBudgetBounds(t *testing.T) {
	b := NewBudget(3, 1)
	for range 10 {
		b.OnFailure()
	}
	if b.Tokens() != 0 || b.AllowRetry() {
		t.Errorf("после 10 неудач: tokens=%v allow=%v, want 0 false", b.Tokens(), b.AllowRetry())
	}
	for range 10 {
		b.OnSuccess()
	}
	if b.Tokens() != 3 || !b.AllowRetry() {
		t.Errorf("после 10 успехов: tokens=%v allow=%v, want 3 true", b.Tokens(), b.AllowRetry())
	}
	z := NewBudget(4, 0)
	z.OnFailure()
	z.OnFailure()
	z.OnSuccess()
	if z.Tokens() != 2 || z.AllowRetry() {
		t.Errorf("ratio 0: tokens=%v allow=%v, want 2 false", z.Tokens(), z.AllowRetry())
	}
}

func TestHiddenBudgetConcurrent(t *testing.T) {
	b := NewBudget(1000, 1)
	for range 500 {
		b.OnFailure()
	}
	// Дальше токенов от 300 до 600 — границы не достигаются, итог однозначен.
	var wg sync.WaitGroup
	for i := range 300 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if i%3 == 0 {
				b.OnSuccess()
			} else {
				b.OnFailure()
			}
			b.AllowRetry()
			b.Tokens()
		}()
	}
	wg.Wait()
	if got := b.Tokens(); got != 400 {
		t.Errorf("после 200 неудач и 100 успехов из горутин tokens=%v, want 400", got)
	}
}

func TestHiddenBackoffEdges(t *testing.T) {
	p := Policy{Base: 100 * time.Millisecond, Max: 3 * time.Second, Rand: half}
	for _, k := range []int{0, -5} {
		if got := p.Backoff(k); got != 50*time.Millisecond {
			t.Errorf("Backoff(%d) = %v, want 50ms (как k = 1)", k, got)
		}
	}
	almost := Policy{Base: time.Millisecond, Max: 3 * time.Second, Rand: func() float64 { return 0.999 }}
	for _, k := range []int{40, 64, 100, 1000, math.MaxInt} {
		if got := almost.Backoff(k); got < 0 || got >= almost.Max {
			t.Errorf("Backoff(%d) = %v, want в [0, %v)", k, got, almost.Max)
		}
		if got := p.Backoff(k); got != 1500*time.Millisecond {
			t.Errorf("Backoff(%d) с rand 0.5 = %v, want 1.5s (половина потолка)", k, got)
		}
	}
	zero := Policy{Base: 100 * time.Millisecond, Max: time.Second, Rand: func() float64 { return 0 }}
	if got := zero.Backoff(3); got != 0 {
		t.Errorf("rand 0: Backoff = %v, want 0", got)
	}
	def := Policy{Base: 100 * time.Millisecond, Max: time.Second}
	for range 50 {
		if got := def.Backoff(2); got < 0 || got >= 200*time.Millisecond {
			t.Fatalf("Rand == nil: Backoff(2) = %v, want в [0, 200ms)", got)
		}
	}
}

func TestHiddenDoCanceledBeforeStart(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	calls := 0
	p := Policy{MaxAttempts: 3, Base: time.Millisecond, Max: time.Second, Rand: half}
	if err := p.Do(ctx, script(&calls)); !errors.Is(err, context.Canceled) {
		t.Errorf("Do с отменённым ctx = %v, want context.Canceled", err)
	}
	if calls != 0 {
		t.Errorf("fn вызвана %d раз, want 0", calls)
	}
}

func TestHiddenDoCanceledBetweenAttempts(t *testing.T) {
	clk := newClock()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	calls := 0
	p := Policy{MaxAttempts: 5, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	err := p.Do(ctx, func(context.Context) error {
		calls++
		cancel() // клиент ушёл во время первой попытки
		return status.Error(codes.Unavailable, "down")
	})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Do = %v, want context.Canceled перед второй попыткой", err)
	}
	if calls != 1 {
		t.Errorf("fn вызвана %d раз, want 1", calls)
	}
}

func TestHiddenDoMaxAttemptsBelowOne(t *testing.T) {
	for _, n := range []int{0, -1} {
		clk := newClock()
		calls := 0
		down := status.Error(codes.Unavailable, "down")
		p := Policy{MaxAttempts: n, Base: time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
		if err := p.Do(context.Background(), script(&calls, down, down)); err != down {
			t.Errorf("MaxAttempts=%d: Do = %v, want %v", n, err, down)
		}
		if calls != 1 || len(clk.sleeps) != 0 {
			t.Errorf("MaxAttempts=%d: вызовов %d, пауз %d; want 1 и 0", n, calls, len(clk.sleeps))
		}
	}
}

func TestHiddenDoReturnsErrorsUnwrapped(t *testing.T) {
	clk := newClock()
	wrapped := fmt.Errorf("reserve: %w", status.Error(codes.Unavailable, "down"))
	calls := 0
	p := Policy{MaxAttempts: 2, Base: time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	if err := p.Do(context.Background(), script(&calls, wrapped, wrapped)); err != wrapped {
		t.Errorf("Do = %v, want тот же объект ошибки", err)
	}
	if calls != 2 {
		t.Errorf("обёрнутый Unavailable тоже повторяется: вызовов %d, want 2", calls)
	}
	plain := errors.New("disk full")
	calls = 0
	if err := p.Do(context.Background(), script(&calls, plain)); err != plain || calls != 1 {
		t.Errorf("ошибка не из gRPC: Do = %v, вызовов %d; want %v и 1", err, calls, plain)
	}
}

func TestHiddenDoBudget(t *testing.T) {
	down := status.Error(codes.Unavailable, "down")
	bad := status.Error(codes.InvalidArgument, "bad")

	// 3 токена из 4: после неудачи остаётся 2 — ровно половина, повторов нет.
	clk := newClock()
	b := NewBudget(4, 0.5)
	b.OnFailure()
	calls := 0
	p := Policy{MaxAttempts: 5, Base: time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep, Budget: b}
	if err := p.Do(context.Background(), script(&calls, down, down)); err != down {
		t.Errorf("пустой бюджет: Do = %v, want %v", err, down)
	}
	if calls != 1 || len(clk.sleeps) != 0 || b.Tokens() != 2 {
		t.Errorf("пустой бюджет: вызовов %d, пауз %d, tokens %v; want 1, 0, 2", calls, len(clk.sleeps), b.Tokens())
	}

	// Не повторяемая ошибка бюджет не трогает.
	calls = 0
	if err := p.Do(context.Background(), script(&calls, bad)); err != bad || b.Tokens() != 2 {
		t.Errorf("InvalidArgument: Do = %v, tokens %v; want %v и 2", err, b.Tokens(), bad)
	}

	// Успех пополняет бюджет, неудачи до него — расходуют.
	full := NewBudget(10, 0.5)
	clk = newClock()
	calls = 0
	p = Policy{MaxAttempts: 5, Base: time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep, Budget: full}
	if err := p.Do(context.Background(), script(&calls, down, down)); err != nil {
		t.Fatalf("Do = %v, want nil", err)
	}
	if calls != 3 || full.Tokens() != 8.5 {
		t.Errorf("вызовов %d, tokens %v; want 3 и 8.5 (10 − 2 + 0.5)", calls, full.Tokens())
	}

	// Последняя попытка тоже снимает токен.
	last := NewBudget(10, 0.5)
	clk = newClock()
	calls = 0
	p = Policy{MaxAttempts: 2, Base: time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep, Budget: last}
	p.Do(context.Background(), script(&calls, down, down, down))
	if last.Tokens() != 8 {
		t.Errorf("2 неудачные попытки: tokens %v, want 8", last.Tokens())
	}
}

func TestHiddenDoRetryInfoBeyondMaxAndDeadline(t *testing.T) {
	// Подсказка сервера используется как есть, даже если больше Max.
	clk := newClock()
	calls := 0
	busy := withRetryInfo(t, codes.ResourceExhausted, durationpb.New(5*time.Second))
	p := Policy{MaxAttempts: 3, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	if err := p.Do(context.Background(), script(&calls, busy)); err != nil {
		t.Fatalf("Do = %v, want nil", err)
	}
	if len(clk.sleeps) != 1 || clk.sleeps[0] != 5*time.Second {
		t.Errorf("паузы %v, want [5s]", clk.sleeps)
	}

	// Подсказка длиннее остатка до дедлайна — ошибка сразу, без паузы.
	clk = newClock()
	calls = 0
	ctx, cancel := context.WithDeadline(context.Background(), clk.Now().Add(2*time.Second))
	defer cancel()
	p = Policy{MaxAttempts: 3, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	if err := p.Do(ctx, script(&calls, busy)); err != busy {
		t.Errorf("Do = %v, want %v", err, busy)
	}
	if calls != 1 || len(clk.sleeps) != 0 {
		t.Errorf("вызовов %d, пауз %d; want 1 и 0", calls, len(clk.sleeps))
	}

	// RetryInfo у не повторяемого кода не делает его повторяемым.
	clk = newClock()
	calls = 0
	badWithHint := withRetryInfo(t, codes.InvalidArgument, durationpb.New(time.Millisecond))
	if err := p.Do(context.Background(), script(&calls, badWithHint)); err != badWithHint || calls != 1 {
		t.Errorf("InvalidArgument с RetryInfo: Do = %v, вызовов %d; want ту же ошибку и 1", err, calls)
	}
}

func TestHiddenDoDeadlineUsesNow(t *testing.T) {
	// Время идёт только через Now: на третьей паузе (200 мс) часы
	// показывают 150 мс из 300 — пауза кончится после дедлайна.
	clk := newClock()
	ctx, cancel := context.WithDeadline(context.Background(), clk.Now().Add(300*time.Millisecond))
	defer cancel()
	calls := 0
	down := status.Error(codes.Aborted, "conflict")
	p := Policy{MaxAttempts: 10, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	if err := p.Do(ctx, script(&calls, down, down, down, down)); err != down {
		t.Errorf("Do = %v, want %v", err, down)
	}
	if calls != 3 || len(clk.sleeps) != 2 {
		t.Errorf("вызовов %d, пауз %v; want 3 и [50ms 100ms]", calls, clk.sleeps)
	}
}

func TestHiddenDoSleepError(t *testing.T) {
	stop := errors.New("sleep interrupted")
	calls := 0
	p := Policy{MaxAttempts: 5, Base: time.Millisecond, Max: time.Second, Rand: half,
		Sleep: func(context.Context, time.Duration) error { return stop }}
	if err := p.Do(context.Background(), script(&calls, status.Error(codes.Unavailable, "down"))); err != stop {
		t.Errorf("Do = %v, want ошибку Sleep как есть", err)
	}
	if calls != 1 {
		t.Errorf("вызовов %d, want 1", calls)
	}
}

func TestHiddenDoDefaults(t *testing.T) {
	// Now == nil — настоящие часы: пауза 1 с не уложится в дедлайн 400 мс.
	slept := false
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	calls := 0
	down := status.Error(codes.Unavailable, "down")
	p := Policy{MaxAttempts: 3, Base: 2 * time.Second, Max: 2 * time.Second, Rand: half,
		Sleep: func(context.Context, time.Duration) error { slept = true; return nil }}
	if err := p.Do(ctx, script(&calls, down, down)); err != down || slept {
		t.Errorf("Now == nil: Do = %v, slept = %v; want %v без паузы", err, slept, down)
	}

	// Sleep == nil — ожидание прерывается отменой контекста.
	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	done := make(chan error, 1)
	p2 := Policy{MaxAttempts: 3, Base: time.Hour, Max: time.Hour, Rand: half}
	go func() {
		done <- p2.Do(ctx2, func(context.Context) error {
			go cancel2()
			return down
		})
	}()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("Sleep == nil: Do = %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Sleep == nil: пауза не прервалась отменой контекста")
	}
}
