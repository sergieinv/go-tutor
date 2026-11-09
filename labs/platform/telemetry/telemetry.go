// Package telemetry настраивает трассировку OpenTelemetry сервиса: провайдер
// спанов, экспорт OTLP/gRPC (в labs — в Jaeger) и пропагатор W3C Trace
// Context, которым контекст трассы переезжает через gRPC-метаданные,
// HTTP-заголовки и заголовки записей Kafka.
//
// Аналог в .NET — services.AddOpenTelemetry().WithTracing(…).AddOtlpExporter().
// Здесь — один вызов Setup в main и явный Shutdown при остановке.
package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// Config — настройки трассировки.
type Config struct {
	ServiceName    string
	ServiceVersion string
	// OTLPEndpoint — host:port приёмника OTLP/gRPC (Jaeger, OpenTelemetry
	// Collector). Пусто — спаны создаются (trace_id есть в журнале и
	// заголовках), но никуда не отправляются.
	OTLPEndpoint string
	// SampleRatio — доля трасс, которые записываются (0..1). Решение
	// принимает корневой сервис, остальные следуют ему (ParentBased).
	SampleRatio float64
	// Logger получает ошибки экспорта: недоступный Jaeger не должен
	// ронять сервис, но и молчать о потере спанов тоже.
	Logger *slog.Logger
}

// Setup создаёт провайдер спанов и делает его глобальным (otel.Tracer,
// otelgrpc, otelhttp берут его оттуда). Вызывающий обязан вызвать
// Shutdown при остановке: он отправляет спаны, накопленные в буфере.
func Setup(ctx context.Context, cfg Config) (*sdktrace.TracerProvider, error) {
	res, err := resource.Merge(resource.Default(), resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(cfg.ServiceName),
		semconv.ServiceVersion(cfg.ServiceVersion),
	))
	if err != nil {
		return nil, fmt.Errorf("otel resource: %w", err)
	}

	opts := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(cfg.SampleRatio))),
	}
	if cfg.OTLPEndpoint != "" {
		// Экспортёр подключается лениво: Jaeger, поднявшийся позже сервиса,
		// не мешает старту. Insecure — приёмник в той же docker-сети; в
		// продакшне — TLS (otlptracegrpc.WithTLSCredentials) до коллектора.
		exp, err := otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint),
			otlptracegrpc.WithInsecure(),
			otlptracegrpc.WithTimeout(5*time.Second),
		)
		if err != nil {
			return nil, fmt.Errorf("otlp exporter: %w", err)
		}
		// Batcher отправляет спаны пачками в фоне: запрос не ждёт экспорта.
		opts = append(opts, sdktrace.WithBatcher(exp))
	}
	tp := sdktrace.NewTracerProvider(opts...)

	otel.SetTracerProvider(tp)
	// Только Trace Context, без Baggage: baggage от внешнего клиента — это
	// произвольные данные, которые поехали бы дальше по всем сервисам.
	otel.SetTextMapPropagator(propagation.TraceContext{})
	if cfg.Logger != nil {
		log := cfg.Logger
		otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
			log.Warn("opentelemetry error", "err", err)
		}))
	}
	return tp, nil
}
