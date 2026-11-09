package task

import (
	"context"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
)

// Имена, справки и бакеты метрик — часть задания, их проверяют тесты.
const (
	HandledName  = "grpc_server_handled_total"
	HandledHelp  = "Завершённые gRPC-вызовы по сервису, методу и коду."
	HandlingName = "grpc_server_handling_seconds"
	HandlingHelp = "Длительность обработки gRPC-вызова в секундах."
	InFlightName = "grpc_server_in_flight"
	InFlightHelp = "gRPC-вызовы, которые обрабатываются сейчас."
)

var Buckets = []float64{0.005, 0.025, 0.1, 0.5, 2.5}

// Metrics — RED-метрики gRPC-сервера.
type Metrics struct {
	// TODO
}

func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	// TODO: создайте и зарегистрируйте метрики
	return &Metrics{}, nil
}

func (m *Metrics) Unary(now func() time.Time) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// TODO: метрики вокруг вызова
		return handler(ctx, req)
	}
}
