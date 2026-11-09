package task

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HeaderCarrier — заголовки записи Kafka как propagation.TextMapCarrier.
type HeaderCarrier struct {
	r *kgo.Record
}

func NewHeaderCarrier(r *kgo.Record) HeaderCarrier {
	return HeaderCarrier{r: r}
}

func (c HeaderCarrier) Get(key string) string {
	// TODO
	return ""
}

func (c HeaderCarrier) Set(key, value string) {
	// TODO
}

func (c HeaderCarrier) Keys() []string {
	// TODO
	return nil
}

// Inject записывает контекст трассы из ctx в заголовки записи.
func Inject(ctx context.Context, p propagation.TextMapPropagator, r *kgo.Record) {
	// TODO
}

// Extract возвращает ctx с контекстом трассы из заголовков записи.
func Extract(ctx context.Context, p propagation.TextMapPropagator, r *kgo.Record) context.Context {
	// TODO
	return ctx
}

// Consume обрабатывает запись в спане потребителя, продолжающем трассу продюсера.
func Consume(ctx context.Context, tracer trace.Tracer, p propagation.TextMapPropagator, r *kgo.Record,
	handle func(ctx context.Context, r *kgo.Record) error) error {
	// TODO
	return handle(ctx, r)
}
