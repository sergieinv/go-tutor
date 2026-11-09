package task

import (
	"context"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HeaderCarrier — заголовки записи Kafka как propagation.TextMapCarrier.
type HeaderCarrier struct {
	r *kgo.Record
}

var _ propagation.TextMapCarrier = HeaderCarrier{}

func NewHeaderCarrier(r *kgo.Record) HeaderCarrier {
	return HeaderCarrier{r: r}
}

func (c HeaderCarrier) Get(key string) string {
	for _, h := range c.r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func (c HeaderCarrier) Set(key, value string) {
	out := c.r.Headers[:0:0]
	replaced := false
	for _, h := range c.r.Headers {
		if h.Key != key {
			out = append(out, h)
			continue
		}
		if !replaced {
			out = append(out, kgo.RecordHeader{Key: key, Value: []byte(value)})
			replaced = true
		}
	}
	if !replaced {
		out = append(out, kgo.RecordHeader{Key: key, Value: []byte(value)})
	}
	c.r.Headers = out
}

func (c HeaderCarrier) Keys() []string {
	var keys []string
	seen := map[string]bool{}
	for _, h := range c.r.Headers {
		if !seen[h.Key] {
			seen[h.Key] = true
			keys = append(keys, h.Key)
		}
	}
	return keys
}

// Inject записывает контекст трассы из ctx в заголовки записи.
func Inject(ctx context.Context, p propagation.TextMapPropagator, r *kgo.Record) {
	p.Inject(ctx, NewHeaderCarrier(r))
}

// Extract возвращает ctx с контекстом трассы из заголовков записи.
func Extract(ctx context.Context, p propagation.TextMapPropagator, r *kgo.Record) context.Context {
	return p.Extract(ctx, NewHeaderCarrier(r))
}

// Consume обрабатывает запись в спане потребителя, продолжающем трассу продюсера.
func Consume(ctx context.Context, tracer trace.Tracer, p propagation.TextMapPropagator, r *kgo.Record,
	handle func(ctx context.Context, r *kgo.Record) error) error {
	ctx = Extract(ctx, p, r)
	ctx, span := tracer.Start(ctx, "process "+r.Topic,
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			attribute.String("messaging.system", "kafka"),
			attribute.String("messaging.operation.type", "process"),
			attribute.String("messaging.destination.name", r.Topic),
			attribute.Int64("messaging.kafka.offset", r.Offset),
		))
	defer span.End()
	if err := handle(ctx, r); err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}
