package task

import (
	"context"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
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
	handled  *prometheus.CounterVec
	handling *prometheus.HistogramVec
	inFlight prometheus.Gauge
}

func NewMetrics(reg prometheus.Registerer) (*Metrics, error) {
	m := &Metrics{
		handled: prometheus.NewCounterVec(prometheus.CounterOpts{Name: HandledName, Help: HandledHelp},
			[]string{"service", "method", "code"}),
		handling: prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: HandlingName, Help: HandlingHelp, Buckets: Buckets},
			[]string{"service", "method"}),
		inFlight: prometheus.NewGauge(prometheus.GaugeOpts{Name: InFlightName, Help: InFlightHelp}),
	}
	for _, c := range []prometheus.Collector{m.handled, m.handling, m.inFlight} {
		if err := reg.Register(c); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// splitMethod разбирает "/pkg.Service/Method".
func splitMethod(full string) (service, method string) {
	rest, ok := strings.CutPrefix(full, "/")
	if !ok {
		return "unknown", "unknown"
	}
	service, method, ok = strings.Cut(rest, "/")
	if !ok || service == "" || method == "" || strings.Contains(method, "/") {
		return "unknown", "unknown"
	}
	return service, method
}

// codeOf — код, который увидит клиент.
func codeOf(err error) codes.Code {
	if err == nil {
		return codes.OK
	}
	if s, ok := status.FromError(err); ok {
		return s.Code()
	}
	return status.FromContextError(err).Code()
}

func (m *Metrics) Unary(now func() time.Time) grpc.UnaryServerInterceptor {
	if now == nil {
		now = time.Now
	}
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		m.inFlight.Inc()
		defer m.inFlight.Dec()
		service, method := splitMethod(info.FullMethod)
		start := now()
		resp, err := handler(ctx, req)
		m.handling.WithLabelValues(service, method).Observe(now().Sub(start).Seconds())
		m.handled.WithLabelValues(service, method, codeOf(err).String()).Inc()
		return resp, err
	}
}
