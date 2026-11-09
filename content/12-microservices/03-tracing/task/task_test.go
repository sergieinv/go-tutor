package task

import (
	"context"
	"slices"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// newTracing — провайдер, который запоминает спаны в памяти.
func newTracing(t *testing.T) (trace.Tracer, *tracetest.SpanRecorder) {
	t.Helper()
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	t.Cleanup(func() { tp.Shutdown(context.Background()) })
	return tp.Tracer("test"), sr
}

func ended(sr *tracetest.SpanRecorder, name string) sdktrace.ReadOnlySpan {
	for _, s := range sr.Ended() {
		if s.Name() == name {
			return s
		}
	}
	return nil
}

func TestCarrier(t *testing.T) {
	r := &kgo.Record{Headers: []kgo.RecordHeader{{Key: "content-type", Value: []byte("application/json")}}}
	c := NewHeaderCarrier(r)
	if got := c.Get("content-type"); got != "application/json" {
		t.Errorf("Get(content-type) = %q", got)
	}
	if got := c.Get("traceparent"); got != "" {
		t.Errorf("Get(traceparent) = %q, want пусто", got)
	}
	c.Set("traceparent", "00-abc-def-01")
	want := []kgo.RecordHeader{
		{Key: "content-type", Value: []byte("application/json")},
		{Key: "traceparent", Value: []byte("00-abc-def-01")},
	}
	if !slices.EqualFunc(r.Headers, want, eqHeader) {
		t.Errorf("Headers = %s, want %s", fmtHeaders(r.Headers), fmtHeaders(want))
	}
	if got := c.Keys(); !slices.Equal(got, []string{"content-type", "traceparent"}) {
		t.Errorf("Keys() = %v", got)
	}
}

func TestTraceContinuesThroughKafka(t *testing.T) {
	tracer, sr := newTracing(t)
	prop := propagation.TraceContext{}

	// Продюсер: спан публикации, контекст — в заголовки.
	ctx, pub := tracer.Start(context.Background(), "publish orders.events", trace.WithSpanKind(trace.SpanKindProducer))
	r := &kgo.Record{Topic: "orders.events", Key: []byte("o-1"), Offset: 7}
	Inject(ctx, prop, r)
	pub.End()

	// Потребитель — «в другом процессе»: только запись.
	err := Consume(context.Background(), tracer, prop, r, func(ctx context.Context, r *kgo.Record) error {
		_, s := tracer.Start(ctx, "send email")
		s.End()
		return nil
	})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	cons := ended(sr, "process orders.events")
	if cons == nil {
		t.Fatalf("нет завершённого спана «process orders.events», есть %d спанов", len(sr.Ended()))
	}
	if cons.SpanKind() != trace.SpanKindConsumer {
		t.Errorf("вид спана %v, want consumer", cons.SpanKind())
	}
	if cons.SpanContext().TraceID() != pub.SpanContext().TraceID() {
		t.Error("спан потребителя в другой трассе")
	}
	if cons.Parent().SpanID() != pub.SpanContext().SpanID() || !cons.Parent().IsRemote() {
		t.Errorf("родитель потребителя %v, want удалённый спан продюсера", cons.Parent().SpanID())
	}
	email := ended(sr, "send email")
	if email == nil || email.Parent().SpanID() != cons.SpanContext().SpanID() {
		t.Error("спан обработчика должен быть ребёнком спана потребителя")
	}
}

func eqHeader(a, b kgo.RecordHeader) bool {
	return a.Key == b.Key && string(a.Value) == string(b.Value)
}

func fmtHeaders(hs []kgo.RecordHeader) string {
	s := "["
	for i, h := range hs {
		if i > 0 {
			s += " "
		}
		s += h.Key + "=" + string(h.Value)
	}
	return s + "]"
}
