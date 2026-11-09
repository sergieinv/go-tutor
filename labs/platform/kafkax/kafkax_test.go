package kafkax

import (
	"context"
	"slices"
	"testing"

	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

func TestHeaderCarrierPropagatesTraceContext(t *testing.T) {
	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := trace.SpanIDFromHex("0102030405060708")
	sc := trace.NewSpanContext(trace.SpanContextConfig{TraceID: traceID, SpanID: spanID, TraceFlags: trace.FlagsSampled})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	r := &kgo.Record{Headers: []kgo.RecordHeader{{Key: "event-id", Value: []byte("e1")}}}
	prop := propagation.TraceContext{}
	prop.Inject(ctx, NewHeaderCarrier(r))
	// Повторная вставка заменяет заголовок, а не дублирует его.
	prop.Inject(ctx, NewHeaderCarrier(r))

	if got := NewHeaderCarrier(r).Keys(); !slices.Equal(got, []string{"event-id", "traceparent"}) {
		t.Fatalf("Keys() = %q", got)
	}
	got := trace.SpanContextFromContext(prop.Extract(context.Background(), NewHeaderCarrier(r)))
	if got.TraceID() != traceID || got.SpanID() != spanID || !got.IsRemote() {
		t.Errorf("extracted %v, want trace %s span %s (remote)", got, traceID, spanID)
	}
	if Header(r, "event-id") != "e1" || Header(r, "missing") != "" {
		t.Errorf("Header: unexpected values")
	}
}

func TestEnsureTopicIsIdempotent(t *testing.T) {
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	cl, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)

	for range 2 {
		if err := EnsureTopic(t.Context(), cl, "orders.events", 3, 1); err != nil {
			t.Fatalf("EnsureTopic: %v", err)
		}
	}
}
