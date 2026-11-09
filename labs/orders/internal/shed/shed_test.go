package shed_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"gotutor/labs/orders/internal/shed"
)

// Тесты без пауз и порогов по времени: таймер ожидания срабатывает по
// команде теста, а момент «вызов встал в очередь» виден по созданию
// таймера.

// fakeTimers — фабрика таймеров: каждый созданный таймер приходит в created.
type fakeTimers struct {
	created chan chan time.Time
}

func newFakeTimers() *fakeTimers { return &fakeTimers{created: make(chan chan time.Time, 16)} }

func (f *fakeTimers) NewTimer(time.Duration) (<-chan time.Time, func() bool) {
	c := make(chan time.Time, 1)
	f.created <- c
	return c, func() bool { return true }
}

type result struct {
	release func()
	reason  string
	err     error
}

func acquireAsync(ctx context.Context, l *shed.Limiter) <-chan result {
	ch := make(chan result, 1)
	go func() {
		release, reason, err := l.Acquire(ctx)
		ch <- result{release, reason, err}
	}()
	return ch
}

func mustAcquire(t *testing.T, l *shed.Limiter) func() {
	t.Helper()
	release, reason, err := l.Acquire(t.Context())
	if release == nil || err != nil {
		t.Fatalf("Acquire = reason %q, err %v; want a slot", reason, err)
	}
	return release
}

func TestFastPathAndRelease(t *testing.T) {
	l := shed.New(shed.Config{MaxInFlight: 2})
	r1 := mustAcquire(t, l)
	mustAcquire(t, l)
	if got := l.InFlight(); got != 2 {
		t.Errorf("InFlight = %d, want 2", got)
	}
	// Мест нет, очереди нет — отказ сразу, без таймера.
	if release, reason, _ := l.Acquire(t.Context()); release != nil || reason != shed.ReasonQueueFull {
		t.Errorf("third Acquire: reason %q, want %q", reason, shed.ReasonQueueFull)
	}
	r1()
	mustAcquire(t, l)
}

func TestQueueTimeout(t *testing.T) {
	timers := newFakeTimers()
	l := shed.New(shed.Config{MaxInFlight: 1, MaxQueue: 1, QueueTimeout: time.Hour, NewTimer: timers.NewTimer})
	mustAcquire(t, l)

	waiter := acquireAsync(t.Context(), l)
	fire := <-timers.created // второй вызов встал в очередь
	if got := l.Waiting(); got != 1 {
		t.Errorf("Waiting = %d, want 1", got)
	}
	// Очередь (1) занята — третий получает отказ сразу.
	if release, reason, _ := l.Acquire(t.Context()); release != nil || reason != shed.ReasonQueueFull {
		t.Errorf("Acquire with full queue: reason %q, want %q", reason, shed.ReasonQueueFull)
	}
	fire <- time.Time{}
	if r := <-waiter; r.release != nil || r.reason != shed.ReasonTimeout {
		t.Errorf("waiter: reason %q, want %q", r.reason, shed.ReasonTimeout)
	}
	if got := l.Waiting(); got != 0 {
		t.Errorf("Waiting after timeout = %d, want 0", got)
	}
}

func TestWaiterGetsFreedSlot(t *testing.T) {
	timers := newFakeTimers()
	l := shed.New(shed.Config{MaxInFlight: 1, MaxQueue: 4, QueueTimeout: time.Hour, NewTimer: timers.NewTimer})
	release := mustAcquire(t, l)

	waiter := acquireAsync(t.Context(), l)
	<-timers.created
	release()
	r := <-waiter
	if r.release == nil || r.err != nil {
		t.Fatalf("waiter: reason %q, err %v; want the freed slot", r.reason, r.err)
	}
	if got := l.InFlight(); got != 1 {
		t.Errorf("InFlight = %d, want 1", got)
	}
}

func TestWaiterContextCancelled(t *testing.T) {
	timers := newFakeTimers()
	l := shed.New(shed.Config{MaxInFlight: 1, MaxQueue: 1, QueueTimeout: time.Hour, NewTimer: timers.NewTimer})
	mustAcquire(t, l)

	ctx, cancel := context.WithCancel(t.Context())
	waiter := acquireAsync(ctx, l)
	<-timers.created
	cancel()
	if r := <-waiter; r.release != nil || !errors.Is(r.err, context.Canceled) {
		t.Errorf("waiter: release %v, err %v; want context.Canceled", r.release != nil, r.err)
	}
}

const (
	limited = "/orders.v1.OrderService/CreateOrder"
	free    = "/orders.v1.OrderService/GetOrder"
)

func TestInterceptor(t *testing.T) {
	reg := prometheus.NewRegistry()
	l := shed.New(shed.Config{MaxInFlight: 1, RetryAfter: 2 * time.Second, Registerer: reg})
	icpt := l.UnaryInterceptor(limited)
	call := func(method string, handler grpc.UnaryHandler) error {
		_, err := icpt(t.Context(), nil, &grpc.UnaryServerInfo{FullMethod: method}, handler)
		return err
	}
	ok := func(context.Context, any) (any, error) { return "ok", nil }

	// Занимаем единственное место вызовом, который ждёт команды теста.
	inHandler, finish := make(chan struct{}), make(chan struct{})
	done := make(chan error, 1)
	go func() {
		done <- call(limited, func(context.Context, any) (any, error) {
			close(inHandler)
			<-finish
			return "ok", nil
		})
	}()
	<-inHandler

	err := call(limited, ok)
	st := status.Convert(err)
	if st.Code() != codes.ResourceExhausted {
		t.Fatalf("code = %v, want ResourceExhausted", st.Code())
	}
	var retry *errdetails.RetryInfo
	for _, d := range st.Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok {
			retry = ri
		}
	}
	if retry == nil || retry.GetRetryDelay().AsDuration() != 2*time.Second {
		t.Errorf("RetryInfo = %v, want 2s", retry)
	}
	// Неограниченный метод проходит, даже когда места нет.
	if err := call(free, ok); err != nil {
		t.Errorf("free method: %v", err)
	}
	close(finish)
	if err := <-done; err != nil {
		t.Errorf("first call: %v", err)
	}
	if err := call(limited, ok); err != nil {
		t.Errorf("after release: %v", err)
	}

	if got := testutil.ToFloat64(mustCounter(t, reg)); got != 1 {
		t.Errorf("orders_shed_rejected_total = %v, want 1", got)
	}
}

func mustCounter(t *testing.T, reg *prometheus.Registry) prometheus.Collector {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() == "orders_shed_rejected_total" {
			v := mf.GetMetric()[0].GetCounter().GetValue()
			return prometheus.NewCounterFunc(prometheus.CounterOpts{Name: "x", Help: "x"}, func() float64 { return v })
		}
	}
	t.Fatal("orders_shed_rejected_total not found")
	return nil
}
