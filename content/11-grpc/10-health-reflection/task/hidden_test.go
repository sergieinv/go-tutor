package task

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// counted считает вызовы проверки.
func counted(n *atomic.Int32, err error) func(context.Context) error {
	return func(context.Context) error {
		n.Add(1)
		return err
	}
}

func TestHiddenEachDependencyCheckedOnce(t *testing.T) {
	hs, hc := newHealth(t)
	var pg, kafka atomic.Int32
	m := NewMonitor(hs,
		[]Dependency{{"postgres", counted(&pg, nil)}, {"kafka", counted(&kafka, errors.New("no brokers"))}},
		map[string][]string{"a": {"postgres", "kafka"}, "b": {"postgres"}, "c": {"kafka", "postgres"}},
		time.Second)
	failed := m.Refresh(context.Background())
	if !slices.Equal(failed, []string{"kafka"}) {
		t.Errorf("Refresh = %q, want [kafka] (без повторов)", failed)
	}
	if pg.Load() != 1 || kafka.Load() != 1 {
		t.Errorf("проверок за Refresh: postgres=%d kafka=%d, want по одной", pg.Load(), kafka.Load())
	}
	want := map[string]string{"": "NOT_SERVING", "a": "NOT_SERVING", "b": "SERVING", "c": "NOT_SERVING"}
	for svc, w := range want {
		if got := statusOf(t, hc, svc); got != w {
			t.Errorf("Check(%q) = %s, want %s", svc, got, w)
		}
	}
}

func TestHiddenRecoveryAndSortedFailures(t *testing.T) {
	hs, hc := newHealth(t)
	var broken atomic.Bool
	broken.Store(true)
	flaky := func(context.Context) error {
		if broken.Load() {
			return errors.New("timeout")
		}
		return nil
	}
	m := NewMonitor(hs,
		[]Dependency{{"redis", flaky}, {"mysql", down}, {"auth-api", flaky}},
		map[string][]string{"x": {"redis", "auth-api"}, "y": {"mysql"}},
		time.Second)
	if got := m.Refresh(context.Background()); !slices.Equal(got, []string{"auth-api", "mysql", "redis"}) {
		t.Errorf("Refresh = %q, want [auth-api mysql redis]", got)
	}
	broken.Store(false)
	if got := m.Refresh(context.Background()); !slices.Equal(got, []string{"mysql"}) {
		t.Errorf("после восстановления Refresh = %q, want [mysql]", got)
	}
	want := map[string]string{"": "NOT_SERVING", "x": "SERVING", "y": "NOT_SERVING"}
	for svc, w := range want {
		if got := statusOf(t, hc, svc); got != w {
			t.Errorf("Check(%q) = %s, want %s", svc, got, w)
		}
	}
}

func TestHiddenTimeout(t *testing.T) {
	hs, hc := newHealth(t)
	hang := func(ctx context.Context) error {
		<-ctx.Done() // зависшая база: ответа нет, пока не отменят
		return ctx.Err()
	}
	m := NewMonitor(hs,
		[]Dependency{{"postgres", hang}, {"kafka", ok}},
		map[string][]string{"orders": {"postgres"}},
		20*time.Millisecond)
	done := make(chan []string, 1)
	go func() { done <- m.Refresh(context.Background()) }()
	select {
	case got := <-done:
		if !slices.Equal(got, []string{"postgres"}) {
			t.Errorf("Refresh = %q, want [postgres]", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Refresh не вернулся: у проверки нет таймаута")
	}
	if got := statusOf(t, hc, "orders"); got != "NOT_SERVING" {
		t.Errorf("Check(orders) = %s, want NOT_SERVING", got)
	}
}

func TestHiddenChecksRunConcurrently(t *testing.T) {
	hs, hc := newHealth(t)
	// Каждая проверка ждёт, пока начнётся другая: последовательный
	// Refresh не дождётся и упрётся в таймаут.
	var started sync.WaitGroup
	started.Add(2)
	both := make(chan struct{})
	go func() { started.Wait(); close(both) }()
	meet := func(ctx context.Context) error {
		started.Done()
		select {
		case <-both:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	m := NewMonitor(hs,
		[]Dependency{{"postgres", meet}, {"kafka", meet}},
		map[string][]string{"orders": {"postgres", "kafka"}},
		2*time.Second)
	if got := m.Refresh(context.Background()); len(got) != 0 {
		t.Errorf("Refresh = %q: проверки выполнялись не параллельно", got)
	}
	if got := statusOf(t, hc, "orders"); got != "SERVING" {
		t.Errorf("Check(orders) = %s, want SERVING", got)
	}
}

func TestHiddenPanicIsFailure(t *testing.T) {
	hs, hc := newHealth(t)
	m := NewMonitor(hs,
		[]Dependency{{"legacy", func(context.Context) error { panic("nil pointer") }}, {"postgres", ok}},
		map[string][]string{"orders": {"postgres"}, "reports": {"legacy"}},
		time.Second)
	if got := m.Refresh(context.Background()); !slices.Equal(got, []string{"legacy"}) {
		t.Errorf("Refresh = %q, want [legacy]", got)
	}
	want := map[string]string{"": "NOT_SERVING", "orders": "SERVING", "reports": "NOT_SERVING"}
	for svc, w := range want {
		if got := statusOf(t, hc, svc); got != w {
			t.Errorf("Check(%q) = %s, want %s", svc, got, w)
		}
	}
}

func TestHiddenUnknownDependency(t *testing.T) {
	hs, hc := newHealth(t)
	m := NewMonitor(hs,
		[]Dependency{{"postgres", ok}},
		map[string][]string{"orders": {"postgres", "s3"}},
		time.Second)
	if got := m.Refresh(context.Background()); !slices.Equal(got, []string{"s3"}) {
		t.Errorf("Refresh = %q, want [s3]", got)
	}
	if got := statusOf(t, hc, "orders"); got != "NOT_SERVING" {
		t.Errorf("Check(orders) = %s, want NOT_SERVING", got)
	}
}

func TestHiddenNoServices(t *testing.T) {
	hs, hc := newHealth(t)
	hs.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)
	var n atomic.Int32
	m := NewMonitor(hs, []Dependency{{"postgres", counted(&n, errors.New("down"))}}, nil, time.Second)
	if got := m.Refresh(context.Background()); !slices.Equal(got, []string{"postgres"}) {
		t.Errorf("Refresh = %q, want [postgres]", got)
	}
	if n.Load() != 1 {
		t.Errorf("проверок %d, want 1: зависимости проверяются, даже если ни одному сервису не нужны", n.Load())
	}
	if got := statusOf(t, hc, ""); got != "SERVING" {
		t.Errorf("Check(\"\") = %s, want SERVING: сервисов нет — все (ноль) обслуживают", got)
	}
}

func TestHiddenWatchSeesChange(t *testing.T) {
	hs, hc := newHealth(t)
	var broken atomic.Bool
	db := func(context.Context) error {
		if broken.Load() {
			return errors.New("down")
		}
		return nil
	}
	m := NewMonitor(hs, []Dependency{{"postgres", db}}, map[string][]string{"orders": {"postgres"}}, time.Second)
	m.Refresh(context.Background())

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	w, err := hc.Watch(ctx, &healthpb.HealthCheckRequest{Service: "orders"})
	if err != nil {
		t.Fatal(err)
	}
	recv := func() string {
		m, err := w.Recv()
		if err != nil {
			t.Fatalf("Watch: %v", err)
		}
		return m.GetStatus().String()
	}
	if got := recv(); got != "SERVING" {
		t.Fatalf("Watch: первый статус %s, want SERVING", got)
	}
	broken.Store(true)
	m.Refresh(context.Background())
	if got := recv(); got != "NOT_SERVING" {
		t.Errorf("Watch после сбоя: %s, want NOT_SERVING", got)
	}
}

func TestHiddenRun(t *testing.T) {
	hs, hc := newHealth(t)
	calls := make(chan struct{}, 10)
	var broken atomic.Bool
	db := func(context.Context) error {
		calls <- struct{}{}
		if broken.Load() {
			return errors.New("down")
		}
		return nil
	}
	m := NewMonitor(hs, []Dependency{{"postgres", db}}, map[string][]string{"orders": {"postgres"}}, time.Second)
	tick := make(chan time.Time)
	ctx, cancel := context.WithCancel(context.Background())
	stopped := make(chan struct{})
	go func() { m.Run(ctx, tick); close(stopped) }()

	wait := func(what string) {
		t.Helper()
		select {
		case <-calls:
		case <-time.After(5 * time.Second):
			t.Fatalf("нет проверки: %s", what)
		}
	}
	wait("Run должен проверить сразу, до первого тика")
	broken.Store(true)
	select {
	case tick <- time.Time{}:
	case <-time.After(5 * time.Second):
		t.Fatal("Run не читает tick")
	}
	wait("после тика")
	// Статус выставляется после проверки — ждём его без жёстких сроков.
	deadline := time.Now().Add(5 * time.Second)
	for statusOf(t, hc, "orders") != "NOT_SERVING" {
		if time.Now().After(deadline) {
			t.Fatal("после тика статус не стал NOT_SERVING")
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run не вернулся после отмены ctx")
	}
}

func TestHiddenRunStopsOnClosedTick(t *testing.T) {
	hs, _ := newHealth(t)
	var n atomic.Int32
	m := NewMonitor(hs, []Dependency{{"postgres", counted(&n, nil)}}, map[string][]string{"orders": {"postgres"}}, time.Second)
	tick := make(chan time.Time)
	close(tick)
	stopped := make(chan struct{})
	go func() { m.Run(context.Background(), tick); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("Run не вернулся после закрытия tick")
	}
	if n.Load() != 1 {
		t.Errorf("проверок %d, want 1: начальный Refresh, а закрытый tick — не тик", n.Load())
	}
}
