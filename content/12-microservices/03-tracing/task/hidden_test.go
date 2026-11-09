package task

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func hdr(kv ...string) []kgo.RecordHeader {
	var hs []kgo.RecordHeader
	for i := 0; i < len(kv); i += 2 {
		hs = append(hs, kgo.RecordHeader{Key: kv[i], Value: []byte(kv[i+1])})
	}
	return hs
}

func TestCarrierDuplicates(t *testing.T) {
	r := &kgo.Record{Headers: hdr("traceparent", "old-1", "a", "1", "traceparent", "old-2", "b", "2")}
	c := NewHeaderCarrier(r)
	if got := c.Get("traceparent"); got != "old-1" {
		t.Errorf("Get при двух заголовках = %q, want первый (old-1)", got)
	}
	if got := c.Keys(); !slices.Equal(got, []string{"traceparent", "a", "b"}) {
		t.Errorf("Keys() = %v, want [traceparent a b] без повторов", got)
	}
	c.Set("traceparent", "new")
	if want := hdr("traceparent", "new", "a", "1", "b", "2"); !slices.EqualFunc(r.Headers, want, eqHeader) {
		t.Errorf("после Set: %s, want %s", fmtHeaders(r.Headers), fmtHeaders(want))
	}
}

func TestCarrierCaseSensitive(t *testing.T) {
	r := &kgo.Record{Headers: hdr("Traceparent", "x")}
	c := NewHeaderCarrier(r)
	if got := c.Get("traceparent"); got != "" {
		t.Errorf("Get(traceparent) нашёл Traceparent: %q — ключи Kafka сравниваются как есть", got)
	}
	c.Set("traceparent", "y")
	if len(r.Headers) != 2 {
		t.Errorf("Set(traceparent) заменил Traceparent: %s", fmtHeaders(r.Headers))
	}
}

func TestCarrierEmptyRecord(t *testing.T) {
	r := &kgo.Record{}
	c := NewHeaderCarrier(r)
	if got := c.Keys(); len(got) != 0 {
		t.Errorf("Keys() пустой записи = %v", got)
	}
	c.Set("k", "v")
	if !slices.EqualFunc(r.Headers, hdr("k", "v"), eqHeader) {
		t.Errorf("Set на записи без заголовков: %s", fmtHeaders(r.Headers))
	}
}

func TestReinjectReplaces(t *testing.T) {
	tracer, _ := newTracing(t)
	prop := propagation.TraceContext{}
	r := &kgo.Record{Topic: "t", Headers: hdr("content-type", "json")}

	ctx1, s1 := tracer.Start(context.Background(), "first")
	Inject(ctx1, prop, r)
	s1.End()
	// Повторная отправка той же записи из другой трассы.
	ctx2, s2 := tracer.Start(context.Background(), "second")
	Inject(ctx2, prop, r)
	s2.End()

	var n int
	for _, h := range r.Headers {
		if h.Key == "traceparent" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("заголовков traceparent: %d, want 1 (%s)", n, fmtHeaders(r.Headers))
	}
	if r.Headers[0].Key != "content-type" {
		t.Errorf("чужой заголовок сдвинут или потерян: %s", fmtHeaders(r.Headers))
	}
	got := trace.SpanContextFromContext(Extract(context.Background(), prop, r))
	if got.TraceID() != s2.SpanContext().TraceID() || got.SpanID() != s2.SpanContext().SpanID() {
		t.Error("Extract вернул не последний внедрённый контекст")
	}
}

func TestBaggageAndValues(t *testing.T) {
	tracer, _ := newTracing(t)
	prop := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})
	m, _ := baggage.NewMember("tenant", "t-1")
	b, _ := baggage.New(m)
	ctx, s := tracer.Start(baggage.ContextWithBaggage(context.Background(), b), "publish")
	r := &kgo.Record{}
	Inject(ctx, prop, r)
	s.End()
	if keys := NewHeaderCarrier(r).Keys(); !slices.Contains(keys, "traceparent") || !slices.Contains(keys, "baggage") {
		t.Fatalf("Keys() = %v, want traceparent и baggage", keys)
	}

	type k struct{}
	base := context.WithValue(context.Background(), k{}, "kept")
	out := Extract(base, prop, r)
	if out.Value(k{}) != "kept" {
		t.Error("Extract потерял значения переданного контекста")
	}
	if got := baggage.FromContext(out).Member("tenant").Value(); got != "t-1" {
		t.Errorf("baggage tenant = %q, want t-1", got)
	}
}

func TestConsumeWithoutHeaders(t *testing.T) {
	tracer, sr := newTracing(t)
	r := &kgo.Record{Topic: "orders.events"}
	var handled *kgo.Record
	var inSpan bool
	err := Consume(context.Background(), tracer, propagation.TraceContext{}, r, func(ctx context.Context, got *kgo.Record) error {
		handled = got
		inSpan = trace.SpanContextFromContext(ctx).IsValid()
		return nil
	})
	if err != nil {
		t.Fatalf("Consume: %v", err)
	}
	if handled != r {
		t.Error("handle получил не ту запись")
	}
	if !inSpan {
		t.Error("handle получил контекст без спана")
	}
	cons := ended(sr, "process orders.events")
	if cons == nil {
		t.Fatal("нет спана потребителя")
	}
	if cons.Parent().IsValid() {
		t.Error("без заголовков спан потребителя должен начинать новую трассу")
	}
}

func TestConsumeAttributes(t *testing.T) {
	tracer, sr := newTracing(t)
	r := &kgo.Record{Topic: "orders.events", Offset: 42, Key: []byte("o-1")}
	if err := Consume(context.Background(), tracer, propagation.TraceContext{}, r,
		func(context.Context, *kgo.Record) error { return nil }); err != nil {
		t.Fatal(err)
	}
	cons := ended(sr, "process orders.events")
	if cons == nil {
		t.Fatal("нет спана потребителя")
	}
	got := attribute.NewSet(cons.Attributes()...)
	want := attribute.NewSet(
		attribute.String("messaging.system", "kafka"),
		attribute.String("messaging.operation.type", "process"),
		attribute.String("messaging.destination.name", "orders.events"),
		attribute.Int64("messaging.kafka.offset", 42),
	)
	if !got.Equals(&want) {
		t.Errorf("атрибуты %v, want %v", got.Encoded(attribute.DefaultEncoder()), want.Encoded(attribute.DefaultEncoder()))
	}
	if cons.SpanKind() != trace.SpanKindConsumer {
		t.Errorf("вид спана %v, want consumer", cons.SpanKind())
	}
	if cons.Status().Code != codes.Unset {
		t.Errorf("статус успешной обработки %v, want Unset", cons.Status().Code)
	}
}

func TestConsumeError(t *testing.T) {
	tracer, sr := newTracing(t)
	errBad := errors.New("bad payload")
	r := &kgo.Record{Topic: "orders.events"}
	err := Consume(context.Background(), tracer, propagation.TraceContext{}, r,
		func(context.Context, *kgo.Record) error { return errBad })
	if !errors.Is(err, errBad) {
		t.Fatalf("Consume err = %v, want ошибку обработчика", err)
	}
	cons := ended(sr, "process orders.events")
	if cons == nil {
		t.Fatal("спан не завершён после ошибки обработчика")
	}
	if cons.Status().Code != codes.Error || cons.Status().Description != "bad payload" {
		t.Errorf("статус %v %q, want Error \"bad payload\"", cons.Status().Code, cons.Status().Description)
	}
	var exc bool
	for _, e := range cons.Events() {
		if e.Name == "exception" {
			exc = true
		}
	}
	if !exc {
		t.Error("ошибка не записана событием exception (span.RecordError)")
	}
}
