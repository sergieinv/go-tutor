package consumer

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/protobuf/proto"

	eventsv1 "gotutor/labs/api/gen/events/v1"
	"gotutor/labs/notifications/internal/notify"
	"gotutor/labs/notifications/internal/store/sqlite"
	"gotutor/labs/platform/kafkax"
	"gotutor/labs/platform/logging"
)

// Тесты идут против kfake — брокера Kafka в памяти процесса из franz-go:
// настоящий протокол, consumer group и коммиты, но без контейнера. Каждый
// тест поднимает свой кластер со своими топиками.

const (
	testTopic = "orders.events"
	testDLQ   = "notifications.dlq"
	testGroup = "notifications"
	// waitTimeout — общий предел ожидания асинхронного результата.
	waitTimeout = 20 * time.Second
)

// newCluster поднимает kfake с топиком событий (3 партиции, как у
// «Заказов») и, если withDLQ, с топиком DLQ.
func newCluster(t *testing.T, withDLQ bool) *kfake.Cluster {
	t.Helper()
	opts := []kfake.Opt{kfake.NumBrokers(1), kfake.SeedTopics(3, testTopic)}
	if withDLQ {
		opts = append(opts, kfake.SeedTopics(1, testDLQ))
	}
	c, err := kfake.NewCluster(opts...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c
}

func newClient(t *testing.T, c *kfake.Cluster, opts ...kgo.Opt) *kgo.Client {
	t.Helper()
	cl, err := kgo.NewClient(append([]kgo.Opt{kgo.SeedBrokers(c.ListenAddrs()...)}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cl.Close)
	return cl
}

func openStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "notifications.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

// harness — запущенный консьюмер и то, что тесту нужно о нём знать.
type harness struct {
	m      *Metrics
	spans  *tracetest.SpanRecorder
	logs   *syncBuffer
	sleeps atomic.Int64
	stop   func()
}

type harnessOpt func(*Config, *[]kgo.Opt)

// start запускает консьюмер группы testGroup над store. stop отменяет Run,
// ждёт его возврата и покидает группу — как main при остановке.
func start(t *testing.T, c *kfake.Cluster, store notify.Store, opts ...harnessOpt) *harness {
	t.Helper()
	h := &harness{spans: tracetest.NewSpanRecorder(), logs: &syncBuffer{}}
	cfg := Config{
		Group:          testGroup,
		DLQTopic:       testDLQ,
		MaxAttempts:    3,
		TracerProvider: sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(h.spans)),
		Propagator:     propagation.TraceContext{},
		// Сон внедрён: повторы без настоящих пауз, но со счётчиком.
		Sleep: func(ctx context.Context, _ time.Duration) error {
			h.sleeps.Add(1)
			return ctx.Err()
		},
	}
	kopts := append(ClientOptions(testTopic, testGroup), kgo.SeedBrokers(c.ListenAddrs()...))
	for _, o := range opts {
		o(&cfg, &kopts)
	}
	cl, err := kgo.NewClient(kopts...)
	if err != nil {
		t.Fatal(err)
	}
	log := logging.New(h.logs, slog.LevelDebug, "notifications")
	h.m = NewMetrics(prometheus.NewRegistry())
	handler := notify.NewHandler(store, time.Now, log)
	cons := New(cl, handler, cfg, log, h.m)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- cons.Run(ctx) }()
	h.stop = sync.OnceFunc(func() {
		cancel()
		if err := <-done; err != nil {
			t.Errorf("Run: %v", err)
		}
		cl.CloseAllowingRebalance()
	})
	t.Cleanup(h.stop)
	return h
}

func (h *harness) events(typ, result string) float64 {
	return testutil.ToFloat64(h.m.events.WithLabelValues(typ, result))
}

// event — запись в формате relay «Заказов».
func event(t *testing.T, ev *eventsv1.OrderEvent) *kgo.Record {
	t.Helper()
	value, err := proto.Marshal(ev)
	if err != nil {
		t.Fatal(err)
	}
	typ := "events.v1.OrderCreated"
	if ev.GetOrderCancelled() != nil {
		typ = "events.v1.OrderCancelled"
	}
	return &kgo.Record{
		Topic: testTopic,
		Key:   []byte(strconv.FormatInt(ev.GetOrderId(), 10)),
		Value: value,
		Headers: []kgo.RecordHeader{
			{Key: "event-id", Value: []byte(ev.GetEventId())},
			{Key: "event-type", Value: []byte(typ)},
			{Key: "content-type", Value: []byte("application/x-protobuf")},
		},
	}
}

func created(eventID string, orderID int64) *eventsv1.OrderEvent {
	return &eventsv1.OrderEvent{
		EventId: eventID,
		OrderId: orderID,
		Event: &eventsv1.OrderEvent_OrderCreated{OrderCreated: &eventsv1.OrderCreated{
			CustomerId: 5, TotalCents: 598000,
		}},
	}
}

func produce(t *testing.T, cl *kgo.Client, rs ...*kgo.Record) {
	t.Helper()
	if err := cl.ProduceSync(t.Context(), rs...).FirstErr(); err != nil {
		t.Fatalf("produce: %v", err)
	}
}

// waitFor ждёт условия, проверяя его в цикле, не дольше waitTimeout.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(waitTimeout)
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		select {
		case <-tick.C:
		case <-t.Context().Done():
			t.Fatalf("test context done waiting for %s", what)
		}
	}
}

// committed — сумма закоммиченных группой офсетов по партициям топика
// событий: сколько записей группа отметила обработанными.
func committed(t *testing.T, admin *kadm.Client) int64 {
	t.Helper()
	offsets, err := admin.FetchOffsets(t.Context(), testGroup)
	if err != nil {
		return -1 // координатор ещё не готов — проверим на следующем шаге
	}
	var sum int64
	offsets.Each(func(o kadm.OffsetResponse) {
		if o.Topic == testTopic && o.Err == nil && o.At > 0 {
			sum += o.At
		}
	})
	return sum
}

func notificationsOf(t *testing.T, s *sqlite.Store, orderID int64) []notify.Notification {
	t.Helper()
	ns, err := s.Notifications(t.Context(), orderID)
	if err != nil {
		t.Fatal(err)
	}
	return ns
}

// readDLQ читает n записей DLQ отдельным клиентом без группы.
func readDLQ(t *testing.T, c *kfake.Cluster, n int) []*kgo.Record {
	t.Helper()
	cl := newClient(t, c, kgo.ConsumeTopics(testDLQ), kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()))
	ctx, cancel := context.WithTimeout(t.Context(), waitTimeout)
	defer cancel()
	var out []*kgo.Record
	for len(out) < n {
		fs := cl.PollFetches(ctx)
		if err := ctx.Err(); err != nil {
			t.Fatalf("read dlq: got %d of %d records", len(out), n)
		}
		out = append(out, fs.Records()...)
	}
	return out
}

func TestOrderCreatedIsSavedAndCommitted(t *testing.T) {
	c := newCluster(t, true)
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	h := start(t, c, store)

	produce(t, prod, event(t, created("e1", 42)))

	waitFor(t, "offset commit", func() bool { return committed(t, admin) == 1 })
	ns := notificationsOf(t, store, 42)
	if len(ns) != 1 || ns[0].Text != "Заказ 42 оформлен на 5 980,00 ₽" || ns[0].CustomerID != 5 {
		t.Fatalf("notifications = %+v", ns)
	}
	if got := h.events(notify.EventOrderCreated, "sent"); got != 1 {
		t.Errorf("events{order_created,sent} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(h.m.sent.WithLabelValues("order_created")); got != 1 {
		t.Errorf("sent{order_created} = %v, want 1", got)
	}
	if !strings.Contains(h.logs.String(), `"msg":"notification sent"`) {
		t.Errorf("no 'notification sent' log record:\n%s", h.logs)
	}
}

func TestDuplicateEventIsProcessedOnce(t *testing.T) {
	c := newCluster(t, true)
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	h := start(t, c, store)

	// Та же запись дважды — как после повторной отправки relay'ем.
	produce(t, prod, event(t, created("e1", 42)), event(t, created("e1", 42)))

	waitFor(t, "both offsets committed", func() bool { return committed(t, admin) == 2 })
	if ns := notificationsOf(t, store, 42); len(ns) != 1 {
		t.Fatalf("notifications = %d, want 1", len(ns))
	}
	if sent, dup := h.events(notify.EventOrderCreated, "sent"), h.events(notify.EventOrderCreated, "duplicate"); sent != 1 || dup != 1 {
		t.Errorf("sent = %v, duplicate = %v; want 1 and 1", sent, dup)
	}
}

func TestUnknownEventIsSkippedAndCommitted(t *testing.T) {
	c := newCluster(t, true)
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	h := start(t, c, store)

	// Событие без варианта oneof — так выглядит событие новой версии
	// контракта для старого потребителя.
	produce(t, prod, event(t, &eventsv1.OrderEvent{EventId: "e1", OrderId: 42}))

	waitFor(t, "offset commit", func() bool { return committed(t, admin) == 1 })
	if got := h.events(notify.EventUnknown, "skipped"); got != 1 {
		t.Errorf("events{unknown,skipped} = %v, want 1", got)
	}
	if ns := notificationsOf(t, store, 42); len(ns) != 0 {
		t.Errorf("notifications = %+v, want none", ns)
	}
}

func TestPoisonRecordGoesToDLQ(t *testing.T) {
	c := newCluster(t, true)
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	h := start(t, c, store)

	poison := &kgo.Record{
		Topic:   testTopic,
		Key:     []byte("42"),
		Value:   []byte("not a protobuf"),
		Headers: []kgo.RecordHeader{{Key: "event-id", Value: []byte("bad")}},
	}
	// Тот же ключ — та же партиция: следующая запись идёт за ядовитой и
	// не должна застрять.
	produce(t, prod, poison, event(t, created("e2", 42)))

	waitFor(t, "both offsets committed", func() bool { return committed(t, admin) == 2 })
	if ns := notificationsOf(t, store, 42); len(ns) != 1 || ns[0].EventID != "e2" {
		t.Fatalf("notifications = %+v, want e2 only", ns)
	}

	r := readDLQ(t, c, 1)[0]
	if !bytes.Equal(r.Key, poison.Key) || !bytes.Equal(r.Value, poison.Value) {
		t.Errorf("dlq record key/value = %q/%q, want the original", r.Key, r.Value)
	}
	want := map[string]string{
		"event-id":           "bad",
		HeaderDLQReason:      "decode",
		HeaderDLQAttempts:    "1",
		HeaderDLQSourceTopic: testTopic,
		// Партиция — та, куда продюсер положил ядовитую запись: её
		// проставил ProduceSync в poison.Partition.
		HeaderDLQSourcePartition: strconv.Itoa(int(poison.Partition)),
		HeaderDLQSourceOffset:    "0",
		HeaderDLQConsumerGroup:   testGroup,
	}
	for k, v := range want {
		if got := kafkax.Header(r, k); got != v {
			t.Errorf("dlq header %s = %q, want %q", k, got, v)
		}
	}
	if got := kafkax.Header(r, HeaderDLQError); !strings.Contains(got, "invalid event") {
		t.Errorf("dlq-error = %q, want decode error text", got)
	}
	if got := h.events(notify.EventUnknown, "dlq"); got != 1 {
		t.Errorf("events{unknown,dlq} = %v, want 1", got)
	}
	if got := testutil.ToFloat64(h.m.dlq.WithLabelValues("decode")); got != 1 {
		t.Errorf("dlq{decode} = %v, want 1", got)
	}
}

// flakyStore падает первые failures вызовов, дальше передаёт настоящему
// хранилищу — как SQLite под чужой блокировкой.
type flakyStore struct {
	notify.Store
	failures int64
	calls    atomic.Int64
}

var errBusy = errors.New("database is locked (SQLITE_BUSY)")

func (s *flakyStore) SaveOnce(ctx context.Context, n notify.Notification) (bool, error) {
	if s.calls.Add(1) <= s.failures {
		return false, errBusy
	}
	return s.Store.SaveOnce(ctx, n)
}

func TestProcessingErrorsAreRetried(t *testing.T) {
	const maxAttempts = 3
	tests := []struct {
		name     string
		failures int64
		wantDLQ  bool
	}{
		{"recovers before attempts run out", maxAttempts - 1, false},
		{"exhausted attempts go to dlq", maxAttempts, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := newCluster(t, true)
			prod := newClient(t, c)
			admin := kadm.NewClient(prod)
			real := openStore(t)
			store := &flakyStore{Store: real, failures: tt.failures}
			h := start(t, c, store, func(cfg *Config, _ *[]kgo.Opt) { cfg.MaxAttempts = maxAttempts })

			produce(t, prod, event(t, created("e1", 42)))
			waitFor(t, "offset commit", func() bool { return committed(t, admin) == 1 })

			ns := notificationsOf(t, real, 42)
			retries := testutil.ToFloat64(h.m.retries)
			if !tt.wantDLQ {
				if len(ns) != 1 || h.events(notify.EventOrderCreated, "sent") != 1 {
					t.Fatalf("notifications = %d, want 1 after retries", len(ns))
				}
				if retries != float64(tt.failures) || h.sleeps.Load() != tt.failures {
					t.Errorf("retries = %v, sleeps = %d; want %d", retries, h.sleeps.Load(), tt.failures)
				}
				return
			}
			if len(ns) != 0 {
				t.Fatalf("notifications = %d, want 0", len(ns))
			}
			if retries != maxAttempts-1 {
				t.Errorf("retries = %v, want %d", retries, maxAttempts-1)
			}
			r := readDLQ(t, c, 1)[0]
			if got := kafkax.Header(r, HeaderDLQReason); got != "processing" {
				t.Errorf("dlq-reason = %q, want processing", got)
			}
			if got := kafkax.Header(r, HeaderDLQAttempts); got != strconv.Itoa(maxAttempts) {
				t.Errorf("dlq-attempts = %q, want %d", got, maxAttempts)
			}
			if got := kafkax.Header(r, HeaderDLQError); !strings.Contains(got, "SQLITE_BUSY") {
				t.Errorf("dlq-error = %q, want the store error", got)
			}
			if got := kafkax.Header(r, "event-id"); got != "e1" {
				t.Errorf("original header event-id = %q, want e1", got)
			}
			if got := h.events(notify.EventOrderCreated, "dlq"); got != 1 {
				t.Errorf("events{order_created,dlq} = %v, want 1", got)
			}
		})
	}
}

// failCommits заставляет брокер отвечать ошибкой на n первых коммитов
// офсетов и считает их.
func failCommits(c *kfake.Cluster, n int64) *atomic.Int64 {
	var calls atomic.Int64
	c.ControlKey(kmsg.OffsetCommit.Int16(), func(kreq kmsg.Request) (kmsg.Response, error, bool) {
		if calls.Add(1) > n {
			c.DropControl()
			return nil, nil, false
		}
		c.KeepControl()
		req := kreq.(*kmsg.OffsetCommitRequest)
		resp := req.ResponseKind().(*kmsg.OffsetCommitResponse)
		resp.Version = req.Version
		for _, rt := range req.Topics {
			st := kmsg.NewOffsetCommitResponseTopic()
			st.Topic, st.TopicID = rt.Topic, rt.TopicID
			for _, rp := range rt.Partitions {
				sp := kmsg.NewOffsetCommitResponseTopicPartition()
				sp.Partition = rp.Partition
				sp.ErrorCode = kerr.UnknownServerError.Code
				st.Partitions = append(st.Partitions, sp)
			}
			resp.Topics = append(resp.Topics, st)
		}
		return resp, nil, true
	})
	return &calls
}

func TestRedeliveryAfterFailedCommitIsDeduplicated(t *testing.T) {
	c := newCluster(t, true)
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	commits := failCommits(c, 1)

	// Первый экземпляр обрабатывает запись, но коммит падает — как если
	// бы процесс умер между обработкой и коммитом.
	first := start(t, c, store)
	produce(t, prod, event(t, created("e1", 42)))
	waitFor(t, "failed commit", func() bool {
		return commits.Load() >= 1 && strings.Contains(first.logs.String(), "commit failed")
	})
	first.stop()
	if got := committed(t, admin); got > 0 {
		t.Fatalf("committed = %d before restart, want 0", got)
	}

	// Перезапуск: та же группа читает с последнего коммита — запись
	// приходит снова и отсеивается как повтор.
	second := start(t, c, store)
	waitFor(t, "offset commit after restart", func() bool { return committed(t, admin) == 1 })
	if got := second.events(notify.EventOrderCreated, "duplicate"); got != 1 {
		t.Errorf("second run duplicate = %v, want 1", got)
	}
	if ns := notificationsOf(t, store, 42); len(ns) != 1 {
		t.Errorf("notifications = %d, want 1", len(ns))
	}
}

func TestTraceContinuesFromHeaders(t *testing.T) {
	c := newCluster(t, true)
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	h := start(t, c, store)

	traceID, _ := trace.TraceIDFromHex("4bf92f3577b34da6a3ce929d0e0e4736")
	parentID, _ := trace.SpanIDFromHex("00f067aa0ba902b7")
	r := event(t, created("e1", 42))
	r.Headers = append(r.Headers, kgo.RecordHeader{
		Key: "traceparent", Value: []byte("00-" + traceID.String() + "-" + parentID.String() + "-01"),
	})
	produce(t, prod, r)
	waitFor(t, "offset commit", func() bool { return committed(t, admin) == 1 })

	var span sdktrace.ReadOnlySpan
	for _, s := range h.spans.Ended() {
		if s.Name() == testTopic+" process" {
			span = s
		}
	}
	if span == nil {
		t.Fatalf("no %q span among %d ended spans", testTopic+" process", len(h.spans.Ended()))
	}
	if span.SpanContext().TraceID() != traceID || span.Parent().SpanID() != parentID {
		t.Errorf("span trace/parent = %s/%s, want %s/%s",
			span.SpanContext().TraceID(), span.Parent().SpanID(), traceID, parentID)
	}
	if span.SpanKind() != trace.SpanKindConsumer {
		t.Errorf("span kind = %v, want consumer", span.SpanKind())
	}
	attrs := map[attribute.Key]string{}
	for _, a := range span.Attributes() {
		attrs[a.Key] = a.Value.Emit()
	}
	for k, v := range map[attribute.Key]string{
		"messaging.system":              "kafka",
		"messaging.destination.name":    testTopic,
		"messaging.consumer.group.name": testGroup,
		"messaging.message.id":          "e1",
		"messaging.kafka.offset":        "0",
	} {
		if attrs[k] != v {
			t.Errorf("attribute %s = %q, want %q", k, attrs[k], v)
		}
	}
	// Журнал «отправки» связан с трассой: тот же trace_id.
	if !strings.Contains(h.logs.String(), `"trace_id":"`+traceID.String()+`"`) {
		t.Errorf("log records lack trace_id %s:\n%s", traceID, h.logs)
	}
}

// Пока DLQ недоступен, запись не коммитится и не пропускается: отправка
// повторяется, а после появления топика запись доходит и коммитится.
func TestDLQUnavailableBlocksUntilItRecovers(t *testing.T) {
	c := newCluster(t, false) // топика DLQ нет
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	h := start(t, c, store, func(_ *Config, kopts *[]kgo.Opt) {
		// Быстрее сдаваться на неизвестном топике, чтобы тест не ждал
		// стандартных повторов метаданных.
		*kopts = append(*kopts, kgo.UnknownTopicRetries(0), kgo.MetadataMinAge(10*time.Millisecond))
	})

	produce(t, prod, &kgo.Record{Topic: testTopic, Key: []byte("1"), Value: []byte("garbage")})
	waitFor(t, "dlq retries", func() bool { return h.sleeps.Load() >= 2 })
	if got := committed(t, admin); got > 0 {
		t.Fatalf("committed = %d while dlq is unavailable, want 0", got)
	}

	if _, err := admin.CreateTopic(t.Context(), 1, 1, nil, testDLQ); err != nil {
		t.Fatal(err)
	}
	waitFor(t, "offset commit", func() bool { return committed(t, admin) == 1 })
	if r := readDLQ(t, c, 1)[0]; kafkax.Header(r, HeaderDLQReason) != "decode" {
		t.Errorf("dlq record reason = %q", kafkax.Header(r, HeaderDLQReason))
	}
}

// Остановка во время бесконечных повторов DLQ не вечна: через
// FinishTimeout Run возвращается, а офсет остаётся незакоммиченным.
func TestShutdownWhileDLQUnavailable(t *testing.T) {
	c := newCluster(t, false)
	prod := newClient(t, c)
	admin := kadm.NewClient(prod)
	store := openStore(t)
	h := start(t, c, store, func(cfg *Config, kopts *[]kgo.Opt) {
		cfg.FinishTimeout = 100 * time.Millisecond
		*kopts = append(*kopts, kgo.UnknownTopicRetries(0), kgo.MetadataMinAge(10*time.Millisecond))
	})

	produce(t, prod, &kgo.Record{Topic: testTopic, Key: []byte("1"), Value: []byte("garbage")})
	waitFor(t, "dlq retries", func() bool { return h.sleeps.Load() >= 1 })
	h.stop() // вернётся только после FinishTimeout; зависание поймает таймаут теста

	if got := committed(t, admin); got > 0 {
		t.Errorf("committed = %d, want 0: record must be redelivered after restart", got)
	}
	if !strings.Contains(h.logs.String(), "dlq produce aborted") {
		t.Errorf("no 'dlq produce aborted' log record:\n%s", h.logs)
	}
}

func TestTruncate(t *testing.T) {
	for _, tt := range []struct {
		s    string
		n    int
		want string
	}{
		{"short", 10, "short"},
		{"abcdef", 3, "abc"},
		{"ошибка", 3, "о"}, // «ш» — 2 байта, не режем посередине
		{"ошибка", 4, "ош"},
	} {
		if got := truncate(tt.s, tt.n); got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.s, tt.n, got, tt.want)
		}
	}
}

// syncBuffer — bytes.Buffer под мьютексом: журнал пишет горутина Run, а
// читает тест.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

var _ io.Writer = (*syncBuffer)(nil)

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}
