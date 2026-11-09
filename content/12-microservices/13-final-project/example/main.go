// Вся система labs/ в одном процессе: «Заказы», «Склад» и
// «Уведомления» говорят по gRPC через `bufconn` и через Kafka
// (`kfake` — брокер в памяти). У «Заказов» своя база SQLite
// с outbox. Трассы собирает экспортёр в памяти, метрики —
// свой реестр Prometheus. Сценарий повторяет curl-сценарий
// из labs/README.md: успех, нехватка товара, компенсация,
// падение склада и выключатель, дубль события и DLQ.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/durationpb"
	_ "modernc.org/sqlite"
)

const (
	topic    = "orders.events"
	dlqTopic = "notifications.dlq"
)

var (
	prop  = propagation.TraceContext{} // W3C traceparent
	reg   = prometheus.NewRegistry()   // свой реестр, а не глобальный
	clock atomic.Int64                 // фейковые часы выключателя, нс
)

// Метрики трёх сервисов (в labs у каждого свой /metrics).
// Метки — только из короткого известного списка значений.
var (
	sagaTotal = counter("orders_saga_total", "result")
	retries   = counter("orders_warehouse_retries_total")
	opened    = counter("orders_warehouse_breaker_opened_total")
	events    = counter("notifications_events_total", "result")
)

func counter(name string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: name}, labels)
	reg.MustRegister(c)
	return c
}

// «Склад» (labs/warehouse): резерв «всё или ничего»,
// идемпотентность по `order_id`, «надгробие» после Release —
// опоздавший Reserve уже отменённого заказа отклоняется.
type warehouse struct {
	UnimplementedWarehouseServiceServer
	mu        sync.Mutex
	available map[int64]int32
	reserved  map[int64][]*Item
	released  map[int64]bool
	down      atomic.Bool  // «docker compose stop warehouse»
	calls     atomic.Int32 // сколько вызовов дошло до склада
}

func (w *warehouse) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	id := req.GetOrderId()
	switch {
	case w.released[id]:
		return nil, status.Error(codes.Aborted, "reservation released")
	case w.reserved[id] != nil:
		return &ReserveResponse{}, nil // повтор — тот же резерв
	}
	for _, it := range req.GetItems() {
		if w.available[it.GetProductId()] < it.GetQuantity() {
			st, _ := status.New(codes.FailedPrecondition, "out of stock").WithDetails(
				&errdetails.ErrorInfo{Reason: "OUT_OF_STOCK", Domain: "warehouse.example"})
			return nil, st.Err()
		}
	}
	for _, it := range req.GetItems() {
		w.available[it.GetProductId()] -= it.GetQuantity()
	}
	w.reserved[id] = req.GetItems()
	return &ReserveResponse{}, nil
}

func (w *warehouse) Release(ctx context.Context, req *ReleaseRequest) (*ReleaseResponse, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, it := range w.reserved[req.GetOrderId()] {
		w.available[it.GetProductId()] += it.GetQuantity()
	}
	delete(w.reserved, req.GetOrderId())
	w.released[req.GetOrderId()] = true
	return &ReleaseResponse{}, nil
}

// fault — interceptor, которым пример «выключает» склад.
func (w *warehouse) fault(ctx context.Context, req any, _ *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
	w.calls.Add(1)
	if w.down.Load() {
		return nil, status.Error(codes.Unavailable, "warehouse is down")
	}
	return h(ctx, req)
}

func (w *warehouse) stock(id int64) string {
	w.mu.Lock()
	defer w.mu.Unlock()
	n := int32(0)
	for _, items := range w.reserved {
		for _, it := range items {
			if it.GetProductId() == id {
				n += it.GetQuantity()
			}
		}
	}
	return fmt.Sprintf("склад: товар %d — доступно %d, в резерве %d", id, w.available[id], n)
}

// Выключатель по неудачам подряд (labs/platform/resilience):
// 5 неудач размыкают его на 5 с, потом один пробный вызов.
var errOpen = errors.New("circuit breaker is open")

type breaker struct {
	mu       sync.Mutex
	state    string
	failures int
	openedAt int64
	probing  bool
}

func (b *breaker) do(ctx context.Context, fn func(context.Context) error) error {
	b.mu.Lock()
	if b.state == "open" && time.Duration(clock.Load()-b.openedAt) >= 5*time.Second {
		b.set("half-open")
	}
	if b.state == "open" || b.state == "half-open" && b.probing {
		b.mu.Unlock()
		return errOpen // сеть не трогаем
	}
	b.probing = b.state == "half-open"
	b.mu.Unlock()

	err := fn(ctx)
	// Нехватка товара — ответ здорового склада, не неудача.
	c := status.Code(err)
	failed := c == codes.Unavailable || c == codes.DeadlineExceeded || c == codes.Internal
	b.mu.Lock()
	defer b.mu.Unlock()
	switch {
	case b.state == "half-open" && failed:
		b.set("open")
	case b.state == "half-open":
		b.set("closed")
	case failed:
		if b.failures++; b.failures >= 5 {
			b.set("open")
		}
	default:
		b.failures = 0
	}
	return err
}

func (b *breaker) set(to string) {
	fmt.Printf("  выключатель: %s → %s\n", b.state, to)
	b.state, b.failures, b.probing = to, 0, false
	if to == "open" {
		b.openedAt = clock.Load()
		opened.WithLabelValues().Inc()
	}
}

// Клиент склада (labs/orders/internal/warehouse): дедлайн на
// вызов → до 3 попыток только на UNAVAILABLE → выключатель.
// Паузы с full jitter в примере опущены, чтобы он шёл быстро.
type warehouseClient struct {
	api WarehouseServiceClient
	br  *breaker
}

func (c *warehouseClient) call(ctx context.Context, fn func(context.Context) error) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	var err error
	for attempt := 1; attempt <= 3; attempt++ {
		if err = c.br.do(ctx, fn); status.Code(err) != codes.Unavailable {
			return err // errOpen — код Unknown: не повторяем
		}
		if attempt < 3 {
			retries.WithLabelValues().Inc()
		}
	}
	return err
}

// «Заказы» (labs/orders/internal/orders): сага из двух шагов.
type orders struct {
	UnimplementedOrderServiceServer
	db        *sql.DB
	wh        *warehouseClient
	failWrite atomic.Bool // внедрённый сбой записи, как в тестах labs
}

func (o *orders) CreateOrder(ctx context.Context, req *CreateOrderRequest) (*CreateOrderResponse, error) {
	// Номер — до резерва: он ключ идемпотентности для склада.
	var id int64
	if err := o.db.QueryRowContext(ctx, `UPDATE seq SET v = v + 1 RETURNING v`).Scan(&id); err != nil {
		return nil, status.Error(codes.Internal, "internal error")
	}
	// Шаг 1: резерв.
	err := o.wh.call(ctx, func(ctx context.Context) error {
		_, err := o.wh.api.Reserve(ctx, &ReserveRequest{OrderId: id, Items: req.GetItems()})
		return err
	})
	switch {
	case outOfStock(err):
		// Точный отказ: резерва нет, компенсировать нечего.
		sagaTotal.WithLabelValues("out_of_stock").Inc()
		st, _ := status.New(codes.FailedPrecondition, "out of stock").WithDetails(
			&errdetails.ErrorInfo{Reason: "OUT_OF_STOCK", Domain: "orders.example"})
		return nil, st.Err()
	case err != nil:
		// Исход неизвестен (таймаут, обрыв): Release безопасен,
		// он идемпотентен и оставляет «надгробие».
		o.release(ctx, id)
		st, _ := status.New(codes.Unavailable, "warehouse unavailable, retry later").WithDetails(
			&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Second)})
		return nil, st.Err()
	}
	// Шаг 2: заказ и событие в outbox одной транзакцией.
	if err := o.save(ctx, id, req.GetCustomerId()); err != nil {
		fmt.Printf("  запись заказа %d не удалась: %v\n", id, err)
		o.release(ctx, id)
		return nil, status.Error(codes.Internal, "internal error") // детали — в журнал
	}
	sagaTotal.WithLabelValues("created").Inc()
	return &CreateOrderResponse{OrderId: id}, nil
}

func outOfStock(err error) bool {
	for _, d := range status.Convert(err).Details() {
		if info, ok := d.(*errdetails.ErrorInfo); ok && info.GetReason() == "OUT_OF_STOCK" {
			return true
		}
	}
	return false
}

// Компенсация — на контексте без отмены: клиент мог уйти,
// а товар вернуть нужно. Своё время — свой таймаут.
func (o *orders) release(ctx context.Context, id int64) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
	defer cancel()
	err := o.wh.call(ctx, func(ctx context.Context) error {
		_, err := o.wh.api.Release(ctx, &ReleaseRequest{OrderId: id})
		return err
	})
	if err != nil {
		// В labs — журнал и алерт; фоновых повторов нет (упражнение).
		sagaTotal.WithLabelValues("release_failed").Inc()
		fmt.Printf("  компенсация Release(%d) не удалась: %v\n", id, status.Convert(err).Message())
		return
	}
	sagaTotal.WithLabelValues("compensated").Inc()
	fmt.Printf("  компенсация Release(%d): товар возвращён\n", id)
}

// В outbox кладём и `traceparent` запроса: relay продолжит
// трассу, хотя опубликует событие позже и в другой горутине.
func (o *orders) save(ctx context.Context, id, customer int64) error {
	tx, err := o.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT INTO orders (id, customer_id) VALUES (?, ?)`, id, customer); err != nil {
		return err
	}
	if o.failWrite.CompareAndSwap(true, false) {
		return errors.New("disk I/O error")
	}
	tc := propagation.MapCarrier{}
	prop.Inject(ctx, tc)
	if _, err := tx.ExecContext(ctx, `INSERT INTO outbox (order_id, customer_id, traceparent) VALUES (?, ?, ?)`,
		id, customer, tc.Get("traceparent")); err != nil {
		return err
	}
	return tx.Commit()
}

// Relay (labs/orders/internal/outbox): пачка по порядку id →
// ProduceSync → отметка только после подтверждения брокера.
func relay(ctx context.Context, db *sql.DB, cl *kgo.Client, tr trace.Tracer, crashBeforeMark bool) {
	rows, err := db.QueryContext(ctx, `SELECT id, order_id, customer_id, traceparent FROM outbox WHERE published = 0 ORDER BY id`)
	if err != nil {
		log.Fatal(err)
	}
	var recs []*kgo.Record
	var spans []trace.Span
	var lastID int64
	for rows.Next() {
		var orderID, customer int64
		var tp string
		if err := rows.Scan(&lastID, &orderID, &customer, &tp); err != nil {
			log.Fatal(err)
		}
		parent := prop.Extract(ctx, propagation.MapCarrier{"traceparent": tp})
		sctx, span := tr.Start(parent, topic+" publish", trace.WithSpanKind(trace.SpanKindProducer))
		spans = append(spans, span)
		eventID := "evt-" + strconv.FormatInt(lastID, 10)
		value, _ := proto.Marshal(&OrderEvent{EventId: eventID, OrderId: orderID, CustomerId: customer})
		// Ключ — номер заказа: события заказа в одной партиции.
		rec := &kgo.Record{Topic: topic, Key: []byte(strconv.FormatInt(orderID, 10)), Value: value,
			Headers: []kgo.RecordHeader{{Key: "event-id", Value: []byte(eventID)}}}
		hc := propagation.MapCarrier{}
		prop.Inject(sctx, hc)
		rec.Headers = append(rec.Headers, kgo.RecordHeader{Key: "traceparent", Value: []byte(hc.Get("traceparent"))})
		recs = append(recs, rec)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	rows.Close()
	err = cl.ProduceSync(ctx, recs...).FirstErr()
	for _, s := range spans {
		s.End()
	}
	if err != nil {
		log.Fatal(err)
	}
	if crashBeforeMark {
		fmt.Printf("relay: отправлено %d, упал до отметки\n", len(recs))
		return
	}
	if _, err := db.ExecContext(ctx, `UPDATE outbox SET published = 1 WHERE published = 0 AND id <= ?`, lastID); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("relay: отправлено %d, отмечено\n", len(recs))
}

// «Уведомления» (labs/notifications): consumer group, ручной
// коммит после обработки, дедупликация по event-id, DLQ.
type notifier struct {
	cl   *kgo.Client
	tr   trace.Tracer
	seen map[string]bool // в labs — processed_events в SQLite
}

func (n *notifier) consume(ctx context.Context, want int) {
	for got := 0; got < want; {
		fetches := n.cl.PollFetches(ctx)
		if err := fetches.Err0(); err != nil {
			log.Fatal(err)
		}
		var done []*kgo.Record
		fetches.EachRecord(func(r *kgo.Record) {
			n.handle(ctx, r)
			done = append(done, r)
		})
		// Коммит — только обработанного (или лежащего в DLQ).
		if err := n.cl.CommitRecords(context.WithoutCancel(ctx), done...); err != nil {
			log.Fatal(err)
		}
		got += len(done)
	}
}

func (n *notifier) handle(ctx context.Context, r *kgo.Record) {
	hc := propagation.MapCarrier{}
	for _, h := range r.Headers {
		hc[h.Key] = string(h.Value)
	}
	ctx, span := n.tr.Start(prop.Extract(ctx, hc), topic+" process", trace.WithSpanKind(trace.SpanKindConsumer))
	defer span.End()
	var ev OrderEvent
	if err := proto.Unmarshal(r.Value, &ev); err != nil || ev.GetEventId() == "" {
		// Ядовитая запись: повтор не поможет — сразу в DLQ.
		d := &kgo.Record{Topic: dlqTopic, Key: r.Key, Value: r.Value, Headers: append(slices.Clone(r.Headers),
			kgo.RecordHeader{Key: "dlq-reason", Value: []byte("decode")},
			kgo.RecordHeader{Key: "dlq-source-offset", Value: []byte(strconv.FormatInt(r.Offset, 10))})}
		if err := n.cl.ProduceSync(ctx, d).FirstErr(); err != nil {
			log.Fatal(err) // без подтверждения DLQ коммитить нельзя
		}
		events.WithLabelValues("dlq").Inc()
		fmt.Printf("  офсет %d → DLQ (decode)\n", r.Offset)
		return
	}
	if n.seen[ev.GetEventId()] {
		span.AddEvent("duplicate")
		events.WithLabelValues("duplicate").Inc()
		fmt.Printf("  офсет %d: %s уже обработано — пропуск\n", r.Offset, ev.GetEventId())
		return
	}
	n.seen[ev.GetEventId()] = true
	events.WithLabelValues("sent").Inc()
	fmt.Printf("  офсет %d: «Заказ %d оформлен» → покупатель %d\n", r.Offset, ev.GetOrderId(), ev.GetCustomerId())
}

func main() {
	ctx := context.Background()
	exp := tracetest.NewInMemoryExporter()
	tpO, tpW, tpN := provider("orders", exp), provider("warehouse", exp), provider("notifications", exp)

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // ":memory:" — своя база у каждого соединения
	if _, err := db.ExecContext(ctx, `
		CREATE TABLE seq (v INTEGER NOT NULL); INSERT INTO seq VALUES (0);
		CREATE TABLE orders (id INTEGER PRIMARY KEY, customer_id INTEGER NOT NULL);
		CREATE TABLE outbox (id INTEGER PRIMARY KEY, order_id INTEGER NOT NULL, customer_id INTEGER NOT NULL,
			traceparent TEXT NOT NULL, published INTEGER NOT NULL DEFAULT 0);`); err != nil {
		log.Fatal(err)
	}

	// Два gRPC-сервера на bufconn; otelgrpc на сервере и клиенте
	// создаёт спаны вызовов и передаёт traceparent в metadata.
	wh := &warehouse{available: map[int64]int32{1: 10, 3: 3}, reserved: map[int64][]*Item{}, released: map[int64]bool{}}
	whSrv, whConn := serve(tpW, tpO, func(s *grpc.Server) { RegisterWarehouseServiceServer(s, wh) }, wh.fault)
	ord := &orders{db: db, wh: &warehouseClient{api: NewWarehouseServiceClient(whConn), br: &breaker{state: "closed"}}}
	ordSrv, ordConn := serve(tpO, tpO, func(s *grpc.Server) { RegisterOrderServiceServer(s, ord) }, nil)
	api := NewOrderServiceClient(ordConn)

	// Kafka в процессе: те же kgo-клиенты, что с настоящим брокером.
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, topic, dlqTopic))
	if err != nil {
		log.Fatal(err)
	}
	defer cluster.Close()
	producer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		log.Fatal(err)
	}
	defer producer.Close()

	// Клиент «как curl»: корневой спан — HTTP-запрос к gateway.
	create := func(title string, customer, product int64, qty int32) {
		fmt.Println("==", title)
		ctx, span := tpO.Tracer("gateway").Start(ctx, "POST /v1/orders", trace.WithSpanKind(trace.SpanKindServer))
		resp, err := api.CreateOrder(ctx, &CreateOrderRequest{CustomerId: customer,
			Items: []*Item{{ProductId: product, Quantity: qty}}})
		span.End()
		if err != nil {
			fmt.Println("  ответ:", describe(err))
		} else {
			fmt.Printf("  ответ: OK, заказ %d\n", resp.GetOrderId())
		}
		fmt.Println("  " + wh.stock(product))
	}

	create("1. Заказ: 2 монитора", 42, 3, 2)
	create("2. Не хватает товара", 7, 3, 2)
	ord.failWrite.Store(true)
	create("3. Сбой записи после резерва", 7, 1, 1)
	wh.down.Store(true)
	create("4. Склад лёг", 8, 1, 1)
	before := wh.calls.Load()
	create("5. Выключатель разомкнут", 8, 1, 1)
	fmt.Printf("  до склада дошло вызовов: %d\n", wh.calls.Load()-before)
	wh.down.Store(false)
	clock.Add(int64(5 * time.Second))
	create("6. Склад поднялся, прошло 5 с", 8, 1, 1)

	fmt.Println("== 7. Outbox → Kafka → «Уведомления»")
	tr := tpO.Tracer("relay")
	relay(ctx, db, producer, tr, true) // at-least-once: дубль
	relay(ctx, db, producer, tr, false)
	// Чужой продюсер записал в топик мусор.
	if err := producer.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: []byte("not a protobuf")}).FirstErr(); err != nil {
		log.Fatal(err)
	}
	consumer, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...),
		kgo.ConsumerGroup("notifications"), kgo.ConsumeTopics(topic), kgo.DisableAutoCommit())
	if err != nil {
		log.Fatal(err)
	}
	cctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	(&notifier{cl: consumer, tr: tpN.Tracer("consumer"), seen: map[string]bool{}}).consume(cctx, 5)
	cancel()
	consumer.Close()

	fmt.Println("== 8. Метрики")
	printMetrics()

	// Остановили серверы — все серверные спаны завершены.
	ordConn.Close()
	ordSrv.GracefulStop()
	whConn.Close()
	whSrv.GracefulStop()
	printTraces(exp.GetSpans())
}

func describe(err error) string {
	st := status.Convert(err)
	out := st.Code().String() + " " + st.Message()
	for _, d := range st.Details() {
		switch d := d.(type) {
		case *errdetails.ErrorInfo:
			out += ", reason " + d.GetReason()
		case *errdetails.RetryInfo:
			out += ", повторить через " + d.GetRetryDelay().AsDuration().String()
		}
	}
	return out
}

// У каждого сервиса свой TracerProvider с `service.name`.
func provider(service string, exp sdktrace.SpanExporter) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(sdktrace.WithSyncer(exp),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))))
}

// serve поднимает gRPC-сервер на bufconn и клиентское соединение
// к нему. Лимит входящего сообщения — «безопасно по умолчанию».
func serve(srvTP, cliTP trace.TracerProvider, register func(*grpc.Server), ic grpc.UnaryServerInterceptor) (*grpc.Server, *grpc.ClientConn) {
	opts := []grpc.ServerOption{grpc.MaxRecvMsgSize(256 << 10),
		grpc.StatsHandler(otelgrpc.NewServerHandler(otelgrpc.WithTracerProvider(srvTP), otelgrpc.WithPropagators(prop)))}
	if ic != nil {
		opts = append(opts, grpc.UnaryInterceptor(ic))
	}
	srv := grpc.NewServer(opts...)
	register(srv)
	lis := bufconn.Listen(1 << 20)
	go srv.Serve(lis)
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()), // mTLS — упражнение урока
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(otelgrpc.WithTracerProvider(cliTP), otelgrpc.WithPropagators(prop))))
	if err != nil {
		log.Fatal(err)
	}
	return srv, conn
}

// Gather отдаёт семейства и ряды отсортированными — вывод стабилен.
func printMetrics() {
	mfs, err := reg.Gather()
	if err != nil {
		log.Fatal(err)
	}
	for _, mf := range mfs {
		for _, m := range mf.GetMetric() {
			var labels []string
			for _, l := range m.GetLabel() {
				labels = append(labels, fmt.Sprintf("%s=%q", l.GetName(), l.GetValue()))
			}
			name := mf.GetName()
			if len(labels) > 0 {
				name += "{" + strings.Join(labels, ",") + "}"
			}
			fmt.Printf("  %s %g\n", name, m.GetCounter().GetValue())
		}
	}
}

// Печатаем трассы заказов 1 и 3: корни — запросы по порядку,
// дети — по времени начала. Без id и времени: они всегда разные.
func printTraces(spans tracetest.SpanStubs) {
	children := map[trace.SpanID][]tracetest.SpanStub{}
	var roots []tracetest.SpanStub
	for _, s := range spans {
		if s.Parent.IsValid() {
			children[s.Parent.SpanID()] = append(children[s.Parent.SpanID()], s)
		} else {
			roots = append(roots, s)
		}
	}
	byStart := func(a, b tracetest.SpanStub) int { return a.StartTime.Compare(b.StartTime) }
	var walk func(s tracetest.SpanStub, depth int)
	walk = func(s tracetest.SpanStub, depth int) {
		svc, _ := s.Resource.Set().Value("service.name")
		line := fmt.Sprintf("%s%s [%s, %s]", strings.Repeat("  ", depth), s.Name, svc.AsString(), s.SpanKind)
		for _, e := range s.Events {
			line += " +" + e.Name
		}
		fmt.Println(line)
		kids := children[s.SpanContext.SpanID()]
		slices.SortFunc(kids, byStart)
		for _, k := range kids {
			walk(k, depth+1)
		}
	}
	slices.SortFunc(roots, byStart)
	for _, i := range []int{0, 2} {
		fmt.Printf("== Трасса заказа %d\n", i+1)
		walk(roots[i], 1)
	}
}
