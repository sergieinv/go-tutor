package task

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// fakeClock — часы, которые двигает обработчик.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// call вызывает interceptor так, как это сделал бы gRPC-сервер.
func call(t *testing.T, ic grpc.UnaryServerInterceptor, method string, h grpc.UnaryHandler) (any, error) {
	t.Helper()
	return ic(context.Background(), "req", &grpc.UnaryServerInfo{FullMethod: method}, h)
}

func newMetrics(t *testing.T) (*Metrics, *prometheus.Registry) {
	t.Helper()
	reg := prometheus.NewRegistry()
	m, err := NewMetrics(reg)
	if err != nil {
		t.Fatalf("NewMetrics: %v", err)
	}
	return m, reg
}

func TestRED(t *testing.T) {
	m, reg := newMetrics(t)
	clk := &fakeClock{t: time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)}
	ic := m.Unary(clk.now)
	const create = "/orders.v1.OrdersService/CreateOrder"

	call(t, ic, create, func(ctx context.Context, req any) (any, error) {
		clk.advance(20 * time.Millisecond)
		return "ok", nil
	})
	call(t, ic, create, func(ctx context.Context, req any) (any, error) {
		clk.advance(300 * time.Millisecond)
		return nil, status.Error(codes.InvalidArgument, "qty must be positive")
	})
	call(t, ic, "/orders.v1.OrdersService/GetOrder", func(ctx context.Context, req any) (any, error) {
		clk.advance(4 * time.Millisecond)
		return nil, status.Error(codes.NotFound, "no such order")
	})

	want := `
# HELP grpc_server_handled_total Завершённые gRPC-вызовы по сервису, методу и коду.
# TYPE grpc_server_handled_total counter
grpc_server_handled_total{code="InvalidArgument",method="CreateOrder",service="orders.v1.OrdersService"} 1
grpc_server_handled_total{code="NotFound",method="GetOrder",service="orders.v1.OrdersService"} 1
grpc_server_handled_total{code="OK",method="CreateOrder",service="orders.v1.OrdersService"} 1
# HELP grpc_server_handling_seconds Длительность обработки gRPC-вызова в секундах.
# TYPE grpc_server_handling_seconds histogram
grpc_server_handling_seconds_bucket{method="CreateOrder",service="orders.v1.OrdersService",le="0.005"} 0
grpc_server_handling_seconds_bucket{method="CreateOrder",service="orders.v1.OrdersService",le="0.025"} 1
grpc_server_handling_seconds_bucket{method="CreateOrder",service="orders.v1.OrdersService",le="0.1"} 1
grpc_server_handling_seconds_bucket{method="CreateOrder",service="orders.v1.OrdersService",le="0.5"} 2
grpc_server_handling_seconds_bucket{method="CreateOrder",service="orders.v1.OrdersService",le="2.5"} 2
grpc_server_handling_seconds_bucket{method="CreateOrder",service="orders.v1.OrdersService",le="+Inf"} 2
grpc_server_handling_seconds_sum{method="CreateOrder",service="orders.v1.OrdersService"} 0.32
grpc_server_handling_seconds_count{method="CreateOrder",service="orders.v1.OrdersService"} 2
grpc_server_handling_seconds_bucket{method="GetOrder",service="orders.v1.OrdersService",le="0.005"} 1
grpc_server_handling_seconds_bucket{method="GetOrder",service="orders.v1.OrdersService",le="0.025"} 1
grpc_server_handling_seconds_bucket{method="GetOrder",service="orders.v1.OrdersService",le="0.1"} 1
grpc_server_handling_seconds_bucket{method="GetOrder",service="orders.v1.OrdersService",le="0.5"} 1
grpc_server_handling_seconds_bucket{method="GetOrder",service="orders.v1.OrdersService",le="2.5"} 1
grpc_server_handling_seconds_bucket{method="GetOrder",service="orders.v1.OrdersService",le="+Inf"} 1
grpc_server_handling_seconds_sum{method="GetOrder",service="orders.v1.OrdersService"} 0.004
grpc_server_handling_seconds_count{method="GetOrder",service="orders.v1.OrdersService"} 1
# HELP grpc_server_in_flight gRPC-вызовы, которые обрабатываются сейчас.
# TYPE grpc_server_in_flight gauge
grpc_server_in_flight 0
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(want)); err != nil {
		t.Error(err)
	}
}

func TestPassThrough(t *testing.T) {
	m, _ := newMetrics(t)
	errWant := status.Error(codes.Unavailable, "db down")
	calls := 0
	resp, err := call(t, m.Unary(nil), "/s.S/M", func(ctx context.Context, req any) (any, error) {
		calls++
		if req != "req" {
			t.Errorf("обработчик получил запрос %v", req)
		}
		return "resp", errWant
	})
	if calls != 1 || resp != "resp" || err != errWant {
		t.Errorf("вызовов %d, ответ %v, ошибка %v; want 1, resp, исходная ошибка", calls, resp, err)
	}
}
