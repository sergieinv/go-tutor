// Трасса — дерево спанов одного запроса через все сервисы.
// Спан — участок работы: имя, вид, время, атрибуты, события
// и статус. Родство передаётся в контексте внутри процесса
// и в заголовке `traceparent` между процессами: в gRPC его
// пишет и читает `otelgrpc`, в сообщение Kafka — мы сами.
// В песочнице спаны собирает экспортёр в памяти, и пример
// печатает дерево — без id и времени, они каждый раз другие.

package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"slices"
	"strings"
	"sync"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/otel/attribute"
	otelcodes "go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

// W3C Trace Context: заголовок `traceparent` с trace id,
// span id родителя и флагом сэмплирования.
var prop = propagation.TraceContext{}

// У каждого сервиса свой TracerProvider с `service.name` в
// ресурсе. `WithSyncer` экспортирует спан сразу при `End` —
// для примера; в проде — `WithBatcher` и OTLP-экспортёр.
func newProvider(service string, exp sdktrace.SpanExporter) *sdktrace.TracerProvider {
	return sdktrace.NewTracerProvider(
		sdktrace.WithSyncer(exp),
		sdktrace.WithResource(resource.NewSchemaless(attribute.String("service.name", service))),
	)
}

type warehouse struct {
	UnimplementedWarehouseServiceServer
	tracer trace.Tracer
	mu     sync.Mutex
	stock  map[string]int32
}

// В `ctx` уже лежит серверный спан от `otelgrpc` — свой
// спан становится его ребёнком. Нехватка товара — не сбой:
// это событие на спане, а не статус Error.
func (w *warehouse) Reserve(ctx context.Context, req *ReserveRequest) (*ReserveResponse, error) {
	_, span := w.tracer.Start(ctx, "UPDATE stock", trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(attribute.String("db.system.name", "mysql")))
	defer span.End()
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, l := range req.GetLines() {
		if w.stock[l.GetSku()] < l.GetQty() {
			span.AddEvent("not enough stock", trace.WithAttributes(attribute.String("sku", l.GetSku())))
			return nil, status.Error(codes.FailedPrecondition, "not enough "+l.GetSku())
		}
	}
	for _, l := range req.GetLines() {
		w.stock[l.GetSku()] -= l.GetQty()
	}
	return &ReserveResponse{}, nil
}

// message — запись Kafka: ключ, значение и заголовки.
type message struct {
	key     string
	headers propagation.MapCarrier
}

type orders struct {
	tracer trace.Tracer
	stock  WarehouseServiceClient
	outbox []message // вместо продюсера Kafka
}

func (o *orders) create(ctx context.Context, id string, qty int32) {
	// Входящий HTTP-запрос: корневой спан трассы.
	ctx, span := o.tracer.Start(ctx, "POST /orders", trace.WithSpanKind(trace.SpanKindServer),
		trace.WithAttributes(attribute.String("order.id", id)))
	defer span.End()
	if _, err := o.stock.Reserve(ctx, &ReserveRequest{OrderId: id, Lines: []*Line{{Sku: "tea", Qty: qty}}}); err != nil {
		span.RecordError(err) // событие "exception" с текстом ошибки
		span.SetStatus(otelcodes.Error, "reserve failed")
		return
	}
	// Продюсер: спан публикации и контекст трассы в заголовках.
	ctx, pub := o.tracer.Start(ctx, "publish orders.events", trace.WithSpanKind(trace.SpanKindProducer))
	h := propagation.MapCarrier{}
	prop.Inject(ctx, h)
	o.outbox = append(o.outbox, message{key: id, headers: h})
	pub.End()
}

// Консьюмер достаёт контекст из заголовков: его спан
// продолжает трассу, хотя прошло сколько угодно времени.
func notify(tracer trace.Tracer, m message) {
	ctx := prop.Extract(context.Background(), m.headers)
	_, span := tracer.Start(ctx, "process orders.events", trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(attribute.String("messaging.kafka.message.key", m.key)))
	span.End()
}

func main() {
	exp := tracetest.NewInMemoryExporter()
	tpOrders, tpWarehouse, tpNotify := newProvider("orders", exp), newProvider("warehouse", exp), newProvider("notifications", exp)
	// Shutdown дописывает буфер экспортёра — без него при
	// выходе из процесса последние спаны потеряются.
	for _, tp := range []*sdktrace.TracerProvider{tpOrders, tpWarehouse, tpNotify} {
		defer tp.Shutdown(context.Background())
	}

	// gRPC: stats handler `otelgrpc` на сервере и клиенте —
	// спаны вызова и `traceparent` в metadata.
	wh := &warehouse{tracer: tpWarehouse.Tracer("warehouse"), stock: map[string]int32{"tea": 3}}
	gs := grpc.NewServer(grpc.StatsHandler(otelgrpc.NewServerHandler(
		otelgrpc.WithTracerProvider(tpWarehouse), otelgrpc.WithPropagators(prop))))
	RegisterWarehouseServiceServer(gs, wh)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go gs.Serve(ln)
	conn, err := grpc.NewClient(ln.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithStatsHandler(otelgrpc.NewClientHandler(
			otelgrpc.WithTracerProvider(tpOrders), otelgrpc.WithPropagators(prop))))
	if err != nil {
		log.Fatal(err)
	}

	o := &orders{tracer: tpOrders.Tracer("orders"), stock: NewWarehouseServiceClient(conn)}
	o.create(context.Background(), "o-1", 2)
	o.create(context.Background(), "o-2", 5) // не хватит товара
	for _, m := range o.outbox {
		tp := strings.Split(m.headers.Get("traceparent"), "-")
		fmt.Printf("заголовки %s: traceparent=%s-<%d hex>-<%d hex>-%s\n", m.key, tp[0], len(tp[1]), len(tp[2]), tp[3])
		notify(tpNotify.Tracer("notifications"), m)
	}

	// Остановили сервер — все серверные спаны завершены.
	conn.Close()
	gs.GracefulStop()
	printTraces(exp.GetSpans())
}

// printTraces строит деревья по родству спанов: корень —
// спан без родителя, дети — по времени начала.
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
		if s.Status.Code == otelcodes.Error {
			line += fmt.Sprintf(" ERROR %q", s.Status.Description)
		}
		for _, e := range s.Events {
			line += " +" + e.Name
		}
		fmt.Println(line)
		kids := children[s.SpanContext.SpanID()]
		slices.SortFunc(kids, byStart)
		for _, k := range kids {
			if k.SpanContext.TraceID() != s.SpanContext.TraceID() {
				panic("ребёнок из другой трассы")
			}
			walk(k, depth+1)
		}
	}
	slices.SortFunc(roots, byStart)
	for i, r := range roots {
		fmt.Printf("трасса %d:\n", i+1)
		walk(r, 1)
	}
}
