package task

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// value — значение серии name с метками labels (k=v через запятую) из реестра.
func value(t *testing.T, reg *prometheus.Registry, name, labels string) (float64, bool) {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			var parts []string
			for _, lp := range m.GetLabel() {
				parts = append(parts, lp.GetName()+"="+lp.GetValue())
			}
			if strings.Join(parts, ",") != labels {
				continue
			}
			switch {
			case m.Counter != nil:
				return m.GetCounter().GetValue(), true
			case m.Gauge != nil:
				return m.GetGauge().GetValue(), true
			case m.Histogram != nil:
				return float64(m.GetHistogram().GetSampleCount()), true
			}
		}
	}
	return 0, false
}

func TestCodes(t *testing.T) {
	m, reg := newMetrics(t)
	ic := m.Unary(nil)
	cases := []struct {
		err  error
		code string
	}{
		{nil, "OK"},
		{status.Error(codes.Unavailable, "down"), "Unavailable"},
		{fmt.Errorf("reserve: %w", status.Error(codes.FailedPrecondition, "no stock")), "FailedPrecondition"},
		{context.DeadlineExceeded, "DeadlineExceeded"},
		{fmt.Errorf("query: %w", context.Canceled), "Canceled"},
		{errors.New("boom"), "Unknown"},
	}
	for _, c := range cases {
		call(t, ic, "/s.S/M", func(context.Context, any) (any, error) { return nil, c.err })
		if v, ok := value(t, reg, HandledName, "code="+c.code+",method=M,service=s.S"); !ok || v != 1 {
			t.Errorf("ошибка %v: счётчик с code=%q = %v (есть: %v), want 1", c.err, c.code, v, ok)
		}
	}
}

func TestMalformedMethod(t *testing.T) {
	m, reg := newMetrics(t)
	ic := m.Unary(nil)
	for _, fm := range []string{"no-slash", "/only-service", "/a/b/c", "//M", ""} {
		call(t, ic, fm, func(context.Context, any) (any, error) { return nil, nil })
	}
	if v, ok := value(t, reg, HandledName, "code=OK,method=unknown,service=unknown"); !ok || v != 5 {
		t.Errorf("кривые имена методов: счётчик unknown/unknown = %v (есть: %v), want 5", v, ok)
	}
	if n := series(t, reg, HandledName); n != 1 {
		t.Errorf("серий %s: %d, want 1 — кривые имена не должны плодить метки", HandledName, n)
	}
}

// series — число серий метрики name в реестре.
func series(t *testing.T, reg *prometheus.Registry, name string) int {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather: %v", err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			return len(mf.GetMetric())
		}
	}
	return 0
}

func TestInFlight(t *testing.T) {
	m, reg := newMetrics(t)
	ic := m.Unary(nil)
	started := make(chan struct{})
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			ic(context.Background(), nil, &grpc.UnaryServerInfo{FullMethod: "/s.S/Slow"}, func(context.Context, any) (any, error) {
				started <- struct{}{}
				<-release
				return nil, nil
			})
		}()
	}
	<-started
	<-started
	if v, _ := value(t, reg, InFlightName, ""); v != 2 {
		t.Errorf("во время двух вызовов in_flight = %v, want 2", v)
	}
	close(release)
	wg.Wait()
	if v, _ := value(t, reg, InFlightName, ""); v != 0 {
		t.Errorf("после вызовов in_flight = %v, want 0", v)
	}
}

func TestInFlightAfterPanic(t *testing.T) {
	m, reg := newMetrics(t)
	ic := m.Unary(nil)
	func() {
		defer func() {
			if recover() == nil {
				t.Error("паника обработчика не дошла до вызывающего")
			}
		}()
		call(t, ic, "/s.S/M", func(context.Context, any) (any, error) { panic("boom") })
	}()
	if v, ok := value(t, reg, InFlightName, ""); !ok || v != 0 {
		t.Errorf("после паники in_flight = %v (есть: %v), want 0", v, ok)
	}
}

func TestRegisterError(t *testing.T) {
	reg := prometheus.NewRegistry()
	if _, err := NewMetrics(reg); err != nil {
		t.Fatalf("первый NewMetrics: %v", err)
	}
	m, err := NewMetrics(reg)
	if err == nil {
		t.Fatal("повторная регистрация в тот же реестр должна вернуть ошибку")
	}
	var are prometheus.AlreadyRegisteredError
	if !errors.As(err, &are) {
		t.Errorf("err = %v, want prometheus.AlreadyRegisteredError", err)
	}
	if m != nil {
		t.Errorf("при ошибке NewMetrics вернул %v, want nil", m)
	}
}

func TestSeparateRegistries(t *testing.T) {
	// Свой реестр на тест: глобальный DefaultRegisterer не трогаем.
	_, reg1 := newMetrics(t)
	m2, reg2 := newMetrics(t)
	call(t, m2.Unary(nil), "/s.S/M", func(context.Context, any) (any, error) { return nil, nil })
	if _, ok := value(t, reg1, HandledName, "code=OK,method=M,service=s.S"); ok {
		t.Error("вызов через второй Metrics попал в первый реестр")
	}
	if v, _ := value(t, reg2, HandlingName, "method=M,service=s.S"); v != 1 {
		t.Errorf("гистограмма во втором реестре: count = %v, want 1", v)
	}
}

func TestConcurrentCalls(t *testing.T) {
	m, reg := newMetrics(t)
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	ic := m.Unary(clk.now)
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			code := codes.OK
			if i%4 == 0 {
				code = codes.Internal
			}
			call(t, ic, "/s.S/M", func(context.Context, any) (any, error) { return nil, status.Error(code, "x") })
		}()
	}
	wg.Wait()
	ok, _ := value(t, reg, HandledName, "code=OK,method=M,service=s.S")
	internal, _ := value(t, reg, HandledName, "code=Internal,method=M,service=s.S")
	count, _ := value(t, reg, HandlingName, "method=M,service=s.S")
	if ok != 30 || internal != 10 || count != 40 {
		t.Errorf("OK=%v Internal=%v count=%v, want 30, 10, 40", ok, internal, count)
	}
}
