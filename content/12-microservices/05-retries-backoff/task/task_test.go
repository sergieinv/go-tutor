package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
)

// fakeClock — управляемое время: Sleep не ждёт, а сдвигает «сейчас».
// Начинается с настоящего времени, чтобы дедлайны контекстов были в будущем.
type fakeClock struct {
	t      time.Time
	sleeps []time.Duration
}

func newClock() *fakeClock { return &fakeClock{t: time.Now()} }

func (c *fakeClock) Now() time.Time { return c.t }

func (c *fakeClock) Sleep(ctx context.Context, d time.Duration) error {
	c.sleeps = append(c.sleeps, d)
	c.t = c.t.Add(d)
	return nil
}

func half() float64 { return 0.5 }

// withRetryInfo — ошибка со статусом code и деталью RetryInfo.
func withRetryInfo(t *testing.T, code codes.Code, d *durationpb.Duration) error {
	t.Helper()
	st, err := status.New(code, "busy").WithDetails(&errdetails.RetryInfo{RetryDelay: d})
	if err != nil {
		t.Fatal(err)
	}
	return st.Err()
}

// script возвращает fn, которая отдаёт ошибки по очереди, затем nil.
func script(calls *int, errs ...error) func(context.Context) error {
	return func(context.Context) error {
		*calls++
		if len(errs) == 0 {
			return nil
		}
		err := errs[0]
		errs = errs[1:]
		return err
	}
}

func TestRetryable(t *testing.T) {
	tests := []struct {
		err  error
		want bool
	}{
		{status.Error(codes.Unavailable, "x"), true},
		{status.Error(codes.ResourceExhausted, "x"), true},
		{status.Error(codes.Aborted, "x"), true},
		{status.Error(codes.InvalidArgument, "x"), false},
		{status.Error(codes.NotFound, "x"), false},
		{status.Error(codes.DeadlineExceeded, "x"), false},
		{errors.New("plain"), false},
		{nil, false},
	}
	for _, tt := range tests {
		if got := Retryable(tt.err); got != tt.want {
			t.Errorf("Retryable(%v) = %v, want %v", tt.err, got, tt.want)
		}
	}
}

func TestRetryDelay(t *testing.T) {
	d, ok := RetryDelay(withRetryInfo(t, codes.ResourceExhausted, durationpb.New(700*time.Millisecond)))
	if !ok || d != 700*time.Millisecond {
		t.Errorf("RetryDelay = %v, %v; want 700ms, true", d, ok)
	}
	if d, ok := RetryDelay(status.Error(codes.Unavailable, "x")); ok || d != 0 {
		t.Errorf("без детали: RetryDelay = %v, %v; want 0, false", d, ok)
	}
}

func TestBudget(t *testing.T) {
	b := NewBudget(4, 0.5)
	if b.Tokens() != 4 || !b.AllowRetry() {
		t.Fatalf("новое ведро: tokens=%v allow=%v, want 4 true", b.Tokens(), b.AllowRetry())
	}
	b.OnFailure()
	b.OnFailure() // 2 — ровно половина: повторы запрещены
	if b.Tokens() != 2 || b.AllowRetry() {
		t.Fatalf("после 2 неудач: tokens=%v allow=%v, want 2 false", b.Tokens(), b.AllowRetry())
	}
	b.OnSuccess() // 2.5
	if b.Tokens() != 2.5 || !b.AllowRetry() {
		t.Fatalf("после успеха: tokens=%v allow=%v, want 2.5 true", b.Tokens(), b.AllowRetry())
	}
}

func TestBackoff(t *testing.T) {
	p := Policy{Base: 100 * time.Millisecond, Max: time.Second, Rand: half}
	var got []time.Duration
	for k := 1; k <= 6; k++ {
		got = append(got, p.Backoff(k))
	}
	want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 500 * time.Millisecond, 500 * time.Millisecond}
	if !slices.Equal(got, want) {
		t.Errorf("Backoff(1..6) = %v, want %v", got, want)
	}
}

func TestDoRetriesUntilSuccess(t *testing.T) {
	clk := newClock()
	p := Policy{MaxAttempts: 5, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	calls := 0
	unavailable := status.Error(codes.Unavailable, "down")
	if err := p.Do(context.Background(), script(&calls, unavailable, unavailable)); err != nil {
		t.Fatalf("Do = %v, want nil", err)
	}
	if calls != 3 {
		t.Errorf("вызовов %d, want 3", calls)
	}
	if want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond}; !slices.Equal(clk.sleeps, want) {
		t.Errorf("паузы %v, want %v", clk.sleeps, want)
	}
}

func TestDoStopsOnNonRetryable(t *testing.T) {
	clk := newClock()
	p := Policy{MaxAttempts: 5, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	calls := 0
	bad := status.Error(codes.InvalidArgument, "quantity <= 0")
	if err := p.Do(context.Background(), script(&calls, bad)); err != bad {
		t.Fatalf("Do = %v, want ту же ошибку %v", err, bad)
	}
	if calls != 1 || len(clk.sleeps) != 0 {
		t.Errorf("вызовов %d, пауз %d; want 1 и 0", calls, len(clk.sleeps))
	}
}

func TestDoAttemptsExhausted(t *testing.T) {
	clk := newClock()
	p := Policy{MaxAttempts: 3, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	calls := 0
	errs := make([]error, 5)
	for i := range errs {
		errs[i] = status.Error(codes.Unavailable, fmt.Sprint("down ", i))
	}
	err := p.Do(context.Background(), script(&calls, errs...))
	if err != errs[2] {
		t.Fatalf("Do = %v, want ошибку последней попытки %v", err, errs[2])
	}
	if calls != 3 || len(clk.sleeps) != 2 {
		t.Errorf("вызовов %d, пауз %d; want 3 и 2", calls, len(clk.sleeps))
	}
}

func TestDoUsesRetryInfo(t *testing.T) {
	clk := newClock()
	p := Policy{MaxAttempts: 3, Base: 100 * time.Millisecond, Max: time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	calls := 0
	busy := withRetryInfo(t, codes.ResourceExhausted, durationpb.New(700*time.Millisecond))
	if err := p.Do(context.Background(), script(&calls, busy)); err != nil {
		t.Fatalf("Do = %v, want nil", err)
	}
	if want := []time.Duration{700 * time.Millisecond}; !slices.Equal(clk.sleeps, want) {
		t.Errorf("паузы %v, want %v", clk.sleeps, want)
	}
}

func TestDoRespectsDeadline(t *testing.T) {
	clk := newClock()
	p := Policy{MaxAttempts: 10, Base: 100 * time.Millisecond, Max: 2 * time.Second, Rand: half, Now: clk.Now, Sleep: clk.Sleep}
	ctx, cancel := context.WithDeadline(context.Background(), clk.Now().Add(600*time.Millisecond))
	defer cancel()
	calls := 0
	errs := make([]error, 10)
	for i := range errs {
		errs[i] = status.Error(codes.Unavailable, fmt.Sprint("down ", i))
	}
	// Паузы 50, 100, 200 (итого 350 мс); следующая, 400, кончится после дедлайна.
	err := p.Do(ctx, script(&calls, errs...))
	if err != errs[3] {
		t.Fatalf("Do = %v, want %v без паузы после 4-й попытки", err, errs[3])
	}
	if want := []time.Duration{50 * time.Millisecond, 100 * time.Millisecond, 200 * time.Millisecond}; !slices.Equal(clk.sleeps, want) {
		t.Errorf("паузы %v, want %v", clk.sleeps, want)
	}
}
