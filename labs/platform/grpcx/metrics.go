package grpcx

import (
	"context"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
)

// ServerMetrics — RED-метрики gRPC-сервера: сколько вызовов, с каким
// кодом и как долго. Имена — как у go-grpc-prometheus: под них есть
// готовые дашборды.
type ServerMetrics struct {
	handled  *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewServerMetrics регистрирует метрики сервера в reg.
func NewServerMetrics(reg prometheus.Registerer) *ServerMetrics {
	m := &ServerMetrics{
		handled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_server_handled_total",
			Help: "Completed gRPC calls by service, method and status code.",
		}, []string{"grpc_service", "grpc_method", "grpc_code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_server_handling_seconds",
			Help:    "gRPC call latency by service and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"grpc_service", "grpc_method"}),
	}
	reg.MustRegister(m.handled, m.duration)
	return m
}

// UnaryInterceptor считает вызовы.
func (m *ServerMetrics) UnaryInterceptor() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		service, method := splitMethod(info.FullMethod)
		m.handled.WithLabelValues(service, method, status.Code(err).String()).Inc()
		m.duration.WithLabelValues(service, method).Observe(time.Since(start).Seconds())
		return resp, err
	}
}

// ClientMetrics — RED-метрики исходящих вызовов: как сервис видит соседа.
// Каждая попытка (в том числе повтор) — отдельный вызов.
type ClientMetrics struct {
	handled  *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewClientMetrics регистрирует метрики клиента в reg.
func NewClientMetrics(reg prometheus.Registerer) *ClientMetrics {
	m := &ClientMetrics{
		handled: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "grpc_client_handled_total",
			Help: "Completed outgoing gRPC calls by service, method and status code.",
		}, []string{"grpc_service", "grpc_method", "grpc_code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "grpc_client_handling_seconds",
			Help:    "Outgoing gRPC call latency by service and method.",
			Buckets: prometheus.DefBuckets,
		}, []string{"grpc_service", "grpc_method"}),
	}
	reg.MustRegister(m.handled, m.duration)
	return m
}

// UnaryInterceptor считает исходящие вызовы.
func (m *ClientMetrics) UnaryInterceptor() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		start := time.Now()
		err := invoker(ctx, method, req, reply, cc, opts...)
		service, name := splitMethod(method)
		m.handled.WithLabelValues(service, name, status.Code(err).String()).Inc()
		m.duration.WithLabelValues(service, name).Observe(time.Since(start).Seconds())
		return err
	}
}

// splitMethod делит "/orders.v1.OrderService/CreateOrder" на сервис и метод.
func splitMethod(full string) (service, method string) {
	full = strings.TrimPrefix(full, "/")
	if i := strings.LastIndex(full, "/"); i >= 0 {
		return full[:i], full[i+1:]
	}
	return "unknown", full
}
