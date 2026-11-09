package outbox_test

import (
	"context"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	eventsv1 "gotutor/labs/api/gen/events/v1"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/outbox"
	"gotutor/labs/orders/internal/store/sqlite"
	"gotutor/labs/orders/internal/storetest"
	"gotutor/labs/platform/kafkax"
)

const topic = "orders.events"

type env struct {
	store    *sqlite.Store
	svc      *orders.Service
	product  orders.Product
	cluster  *kfake.Cluster
	producer *kgo.Client
	reg      *prometheus.Registry
	relay    *outbox.Relay
}

func newEnv(t *testing.T) *env {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "orders.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	p, err := s.CreateProduct(t.Context(), orders.NewProduct{Name: "Ручка", PriceCents: 100})
	if err != nil {
		t.Fatal(err)
	}
	// Брокер Kafka в процессе: один узел, топик из трёх партиций.
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(3, topic))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cluster.Close)
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.RecordDeliveryTimeout(2*time.Second))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(producer.Close)
	reg := prometheus.NewRegistry()
	e := &env{
		store:    s,
		svc:      orders.NewService(s, storetest.NewWarehouse(), orders.Options{}),
		product:  p,
		cluster:  cluster,
		producer: producer,
		reg:      reg,
	}
	e.relay = outbox.New(s, producer, outbox.Config{Topic: topic, Registerer: reg})
	return e
}

func (e *env) createOrder(t *testing.T, ctx context.Context) orders.Order {
	t.Helper()
	o, err := e.svc.CreateOrder(ctx, orders.CreateOrderRequest{CustomerID: 7, Items: []orders.ItemRequest{{ProductID: e.product.ID, Quantity: 1}}})
	if err != nil {
		t.Fatal(err)
	}
	return o
}

// consume читает n записей топика с начала.
func consume(t *testing.T, cluster *kfake.Cluster, n int) []*kgo.Record {
	t.Helper()
	cl, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	if err != nil {
		t.Fatal(err)
	}
	defer cl.Close()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	var recs []*kgo.Record
	for len(recs) < n {
		fetches := cl.PollFetches(ctx)
		if ctx.Err() != nil {
			t.Fatalf("got %d records, want %d", len(recs), n)
		}
		recs = append(recs, fetches.Records()...)
	}
	return recs
}

func TestRelayPublishesWithKeyHeadersAndPayload(t *testing.T) {
	e := newEnv(t)
	o1 := e.createOrder(t, t.Context())
	if _, err := e.svc.CancelOrder(t.Context(), o1.ID); err != nil {
		t.Fatal(err)
	}
	o2 := e.createOrder(t, t.Context())

	n, err := e.relay.PublishOnce(t.Context())
	if err != nil || n != 3 {
		t.Fatalf("PublishOnce = %d, %v; want 3", n, err)
	}
	if len(storetest.PendingEvents(t, e.store)) != 0 {
		t.Error("events are still pending after publish")
	}

	recs := consume(t, e.cluster, 3)
	byOrder := map[string][]string{}
	partitions := map[string]int32{}
	for _, r := range recs {
		var ev eventsv1.OrderEvent
		if err := proto.Unmarshal(r.Value, &ev); err != nil {
			t.Fatalf("value: %v", err)
		}
		key := string(r.Key)
		if key != strconv.FormatInt(ev.GetOrderId(), 10) {
			t.Errorf("key %q != order id %d", key, ev.GetOrderId())
		}
		if kafkax.Header(r, outbox.HeaderEventID) != ev.GetEventId() ||
			kafkax.Header(r, outbox.HeaderContentType) != "application/x-protobuf" {
			t.Errorf("headers = %v", r.Headers)
		}
		// Все события заказа — в одной партиции, по порядку.
		if p, ok := partitions[key]; ok && p != r.Partition {
			t.Errorf("order %s events in partitions %d and %d", key, p, r.Partition)
		}
		partitions[key] = r.Partition
		byOrder[key] = append(byOrder[key], kafkax.Header(r, outbox.HeaderEventType))
	}
	k1, k2 := strconv.FormatInt(o1.ID, 10), strconv.FormatInt(o2.ID, 10)
	if got := byOrder[k1]; len(got) != 2 || got[0] != orders.EventOrderCreated || got[1] != orders.EventOrderCancelled {
		t.Errorf("order %s events = %v, want created then cancelled", k1, got)
	}
	if got := byOrder[k2]; len(got) != 1 || got[0] != orders.EventOrderCreated {
		t.Errorf("order %s events = %v", k2, got)
	}
	if v := metricValue(t, e.reg, "orders_outbox_published_total"); v != 3 {
		t.Errorf("published metric = %v", v)
	}
}

func TestRelayKeepsEventsWhenBrokerIsDown(t *testing.T) {
	e := newEnv(t)
	e.createOrder(t, t.Context())
	e.cluster.Close() // брокер недоступен

	n, err := e.relay.PublishOnce(t.Context())
	if err == nil || n != 0 {
		t.Fatalf("PublishOnce = %d, %v; want error", n, err)
	}
	if got := len(storetest.PendingEvents(t, e.store)); got != 1 {
		t.Errorf("pending = %d, want 1: event must stay in outbox", got)
	}
	if v := metricValue(t, e.reg, "orders_outbox_publish_errors_total"); v != 1 {
		t.Errorf("errors metric = %v", v)
	}
	if v := metricValue(t, e.reg, "orders_outbox_pending_events"); v != 1 {
		t.Errorf("pending metric = %v", v)
	}
}

func TestRelayContinuesTrace(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(rec))
	prevTP, prevProp := otel.GetTracerProvider(), otel.GetTextMapPropagator()
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	t.Cleanup(func() { otel.SetTracerProvider(prevTP); otel.SetTextMapPropagator(prevProp) })

	e := newEnv(t)
	// «Запрос» создания заказа со своим спаном: его контекст попадает в outbox.
	reqCtx, reqSpan := tp.Tracer("test").Start(t.Context(), "CreateOrder")
	e.createOrder(t, reqCtx)
	reqSpan.End()

	if _, err := e.relay.PublishOnce(t.Context()); err != nil {
		t.Fatal(err)
	}
	var publish sdktrace.ReadOnlySpan
	for _, s := range rec.Ended() {
		if s.Name() == topic+" publish" {
			publish = s
		}
	}
	if publish == nil {
		t.Fatal("no publish span")
	}
	if publish.Parent().SpanID() != reqSpan.SpanContext().SpanID() || publish.SpanKind() != trace.SpanKindProducer {
		t.Errorf("publish span parent = %v kind = %v; want child of request span", publish.Parent().SpanID(), publish.SpanKind())
	}
	// В заголовке записи — контекст спана публикации.
	r := consume(t, e.cluster, 1)[0]
	got := trace.SpanContextFromContext(propagation.TraceContext{}.Extract(context.Background(), kafkax.NewHeaderCarrier(r)))
	if got.TraceID() != reqSpan.SpanContext().TraceID() || got.SpanID() != publish.SpanContext().SpanID() {
		t.Errorf("record traceparent = %v, want publish span of the same trace", got)
	}
}

func TestRunDrainsOnStop(t *testing.T) {
	e := newEnv(t)
	relay := outbox.New(e.store, e.producer, outbox.Config{Topic: topic, Interval: time.Hour})
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- relay.Run(ctx) }()
	e.createOrder(t, t.Context()) // появилось после первого такта
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	// Последний проход при остановке опубликовал событие.
	if n := len(storetest.PendingEvents(t, e.store)); n != 0 {
		t.Errorf("pending after stop = %d, want 0", n)
	}
	consume(t, e.cluster, 1)
}

// metricValue — значение счётчика или датчика без меток из реестра теста.
func metricValue(t *testing.T, reg *prometheus.Registry, name string) float64 {
	t.Helper()
	mfs, err := reg.Gather()
	if err != nil {
		t.Fatal(err)
	}
	for _, mf := range mfs {
		if mf.GetName() == name {
			m := mf.GetMetric()[0]
			return m.GetCounter().GetValue() + m.GetGauge().GetValue()
		}
	}
	t.Fatalf("metric %s not found", name)
	return 0
}
