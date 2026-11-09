package task

import (
	"context"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/status"
)

func TestAttemptsExhaustedReturnsLastError(t *testing.T) {
	ft := newFakeTime()
	client, srv := start(t, config(ft, 3),
		status.Error(codes.Unavailable, "e1"),
		status.Error(codes.Unavailable, "e2"),
		status.Error(codes.Unavailable, "e3"),
		status.Error(codes.Unavailable, "e4"))
	_, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{})
	st := status.Convert(err)
	if st.Code() != codes.Unavailable || st.Message() != "e3" {
		t.Fatalf("err = %v, want Unavailable e3 (ошибка последней попытки)", err)
	}
	if n := srv.count(); n != 3 {
		t.Fatalf("попыток = %d, want 3", n)
	}
	if got := ft.slept(); len(got) != 2 {
		t.Fatalf("пауз = %v, want 2 (после последней попытки не спим)", got)
	}
}

func TestMaxAttemptsBelowOne(t *testing.T) {
	for _, n := range []int{0, -2} {
		ft := newFakeTime()
		client, srv := start(t, config(ft, n), unavailable(), unavailable())
		_, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{})
		if status.Code(err) != codes.Unavailable || srv.count() != 1 || len(ft.slept()) != 0 {
			t.Fatalf("MaxAttempts = %d: err = %v, попыток = %d, пауз = %v; want Unavailable, 1, 0", n, err, srv.count(), ft.slept())
		}
	}
}

func TestBackoffCap(t *testing.T) {
	ft := newFakeTime()
	cfg := config(ft, 6)
	cfg.InitialBackoff = 300 * time.Millisecond
	client, _ := start(t, cfg, unavailable(), unavailable(), unavailable(), unavailable(), unavailable())
	if _, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	want := []time.Duration{300 * time.Millisecond, 600 * time.Millisecond, time.Second, time.Second, time.Second}
	if got := ft.slept(); !slices.Equal(got, want) {
		t.Fatalf("паузы = %v, want %v", got, want)
	}
}

func TestBackoffWithoutCap(t *testing.T) {
	ft := newFakeTime()
	cfg := config(ft, 5)
	cfg.MaxBackoff = 0
	client, _ := start(t, cfg, unavailable(), unavailable(), unavailable(), unavailable())
	if _, err := client.Check(context.Background(), &healthpb.HealthCheckRequest{}); err != nil {
		t.Fatalf("Check: %v", err)
	}
	want := []time.Duration{100 * time.Millisecond, 200 * time.Millisecond, 400 * time.Millisecond, 800 * time.Millisecond}
	if got := ft.slept(); !slices.Equal(got, want) {
		t.Fatalf("паузы = %v, want %v", got, want)
	}
}

func TestRetryableCodes(t *testing.T) {
	tests := []struct {
		name  string
		err   error
		sleep time.Duration
	}{
		{"Aborted", status.Error(codes.Aborted, "conflict"), 100 * time.Millisecond},
		{"ResourceExhausted с RetryInfo", withRetryInfo(codes.ResourceExhausted, 2*time.Second), 2 * time.Second},
		{"Unavailable с отрицательным RetryInfo", withRetryInfo(codes.Unavailable, -time.Second), 0},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := newFakeTime()
			client, srv := start(t, config(ft, 2), tt.err)
			if _, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{}); err != nil {
				t.Fatalf("Check: %v, want успех на второй попытке", err)
			}
			if srv.count() != 2 {
				t.Fatalf("попыток = %d, want 2", srv.count())
			}
			if got := ft.slept(); !slices.Equal(got, []time.Duration{tt.sleep}) {
				t.Fatalf("паузы = %v, want [%v]", got, tt.sleep)
			}
		})
	}
}

func TestRetryInfoDoesNotMakeRetryable(t *testing.T) {
	ft := newFakeTime()
	client, srv := start(t, config(ft, 3), withRetryInfo(codes.InvalidArgument, time.Second))
	_, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{})
	if status.Code(err) != codes.InvalidArgument || srv.count() != 1 {
		t.Fatalf("err = %v, попыток = %d; want InvalidArgument, 1", err, srv.count())
	}
}

func TestDetailsPreserved(t *testing.T) {
	st, _ := status.New(codes.InvalidArgument, "bad request").WithDetails(&errdetails.BadRequest{
		FieldViolations: []*errdetails.BadRequest_FieldViolation{{Field: "sku", Description: "required"}},
	})
	ft := newFakeTime()
	client, _ := start(t, config(ft, 3), st.Err())
	_, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{})
	details := status.Convert(err).Details()
	if len(details) != 1 {
		t.Fatalf("детали ошибки = %v, want BadRequest", details)
	}
	if br, ok := details[0].(*errdetails.BadRequest); !ok || br.GetFieldViolations()[0].GetField() != "sku" {
		t.Fatalf("детали ошибки = %v, want BadRequest sku", details)
	}
}

func TestDeadlineTooShortForRetryInfo(t *testing.T) {
	ft := newFakeTime()
	client, srv := start(t, config(ft, 3), withRetryInfo(codes.Unavailable, 3*time.Second))
	ctx, cancel := context.WithDeadline(context.Background(), ft.Now().Add(2*time.Second))
	defer cancel()
	_, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
	if status.Code(err) != codes.Unavailable || srv.count() != 1 || len(ft.slept()) != 0 {
		t.Fatalf("err = %v, попыток = %d, пауз = %v; want Unavailable, 1, 0 — ждать дольше дедлайна бессмысленно", err, srv.count(), ft.slept())
	}
}

func TestDeadlineEqualToDelay(t *testing.T) {
	ft := newFakeTime()
	client, srv := start(t, config(ft, 3), withRetryInfo(codes.Unavailable, 2*time.Second))
	ctx, cancel := context.WithDeadline(context.Background(), ft.Now().Add(2*time.Second))
	defer cancel()
	_, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
	if status.Code(err) != codes.Unavailable || srv.count() != 1 || len(ft.slept()) != 0 {
		t.Fatalf("err = %v, попыток = %d, пауз = %v; want Unavailable, 1, 0", err, srv.count(), ft.slept())
	}
}

func TestDeadlineCutsBackoff(t *testing.T) {
	ft := newFakeTime()
	cfg := config(ft, 5)
	cfg.InitialBackoff = time.Second
	cfg.MaxBackoff = 0
	client, srv := start(t, cfg, unavailable(), unavailable(), unavailable())
	ctx, cancel := context.WithDeadline(context.Background(), ft.Now().Add(2500*time.Millisecond))
	defer cancel()
	_, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
	// Пауза 1 с помещается (осталось 2,5 с), следующая 2 с — уже нет (осталось 1,5 с).
	if status.Code(err) != codes.Unavailable || srv.count() != 2 || !slices.Equal(ft.slept(), []time.Duration{time.Second}) {
		t.Fatalf("err = %v, попыток = %d, пауз = %v; want Unavailable, 2, [1s]", err, srv.count(), ft.slept())
	}
}

func TestSleepCancelled(t *testing.T) {
	for _, e := range []struct {
		err  error
		code codes.Code
	}{{context.Canceled, codes.Canceled}, {context.DeadlineExceeded, codes.DeadlineExceeded}} {
		ft := newFakeTime()
		ft.err = e.err
		client, srv := start(t, config(ft, 3), unavailable(), unavailable())
		_, err := client.Check(ctx5s(t), &healthpb.HealthCheckRequest{})
		if status.Code(err) != e.code || srv.count() != 1 {
			t.Fatalf("Sleep вернул %v: err = %v, попыток = %d; want %v, 1", e.err, err, srv.count(), e.code)
		}
	}
}

func TestConcurrentCalls(t *testing.T) {
	ft := newFakeTime()
	client, srv := start(t, config(ft, 3))
	ctx := ctx5s(t)
	var wg sync.WaitGroup
	errs := make(chan error, 8)
	for range 8 {
		wg.Go(func() {
			_, err := client.Check(ctx, &healthpb.HealthCheckRequest{})
			errs <- err
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("Check: %v", err)
		}
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for _, a := range srv.attempts {
		if !slices.Equal(a, []string{"1"}) {
			t.Fatalf("x-attempt = %v, want [1]", a)
		}
	}
}
