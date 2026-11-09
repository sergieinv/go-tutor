// Package outbox — relay transactional outbox: переносит события из
// таблицы outbox в Kafka.
//
// Почему не отправлять в Kafka прямо из обработчика запроса: запись в базу
// и отправка в брокер — две разные системы, атомарно их не сделать.
// «Записали заказ, упали до отправки» — событие потеряно; «отправили,
// а транзакция откатилась» — событие о заказе, которого нет. Outbox
// превращает отправку в запись той же транзакцией, а relay доставляет её
// позже — at-least-once: событие может уйти дважды, но не потеряется.
// В .NET то же делает outbox MassTransit или NServiceBus.
package outbox

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/platform/kafkax"
)

// Store — то, что relay нужно от хранилища (реализуют postgres.Store и
// sqlite.Store).
type Store interface {
	// PublishPending берёт до limit неопубликованных событий по порядку,
	// вызывает publish и отмечает их опубликованными, только если publish
	// вернул nil. Возвращает число опубликованных.
	PublishPending(ctx context.Context, limit int, publish func(context.Context, []orders.Event) error) (int, error)
	// PendingStats — число ждущих событий и время создания самого старого.
	PendingStats(ctx context.Context) (int64, time.Time, error)
}

// Producer — то, что relay нужно от *kgo.Client.
type Producer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
}

// Config — настройки relay.
type Config struct {
	Topic string
	// BatchSize — сколько событий за такт; 0 — 100.
	BatchSize int
	// Interval — пауза между тактами, когда ждущих событий не осталось;
	// 0 — 500 мс. Это и есть типичная задержка публикации.
	Interval time.Duration
	// BatchTimeout — на один такт (выборка, отправка, отметка); 0 — 15 с.
	BatchTimeout time.Duration
	Logger       *slog.Logger
	// Registerer — куда зарегистрировать метрики; nil — без метрик.
	Registerer prometheus.Registerer
	// Now — часы для метрики отставания; nil — time.Now.
	Now func() time.Time
}

// Relay публикует события outbox в Kafka.
type Relay struct {
	store    Store
	producer Producer
	cfg      Config
	tracer   trace.Tracer
	m        *metrics
}

// New создаёт relay.
func New(store Store, producer Producer, cfg Config) *Relay {
	if cfg.BatchSize == 0 {
		cfg.BatchSize = 100
	}
	if cfg.Interval == 0 {
		cfg.Interval = 500 * time.Millisecond
	}
	if cfg.BatchTimeout == 0 {
		cfg.BatchTimeout = 15 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	return &Relay{
		store:    store,
		producer: producer,
		cfg:      cfg,
		tracer:   otel.Tracer("gotutor/labs/orders/internal/outbox"),
		m:        newMetrics(cfg.Registerer),
	}
}

// Run публикует события до отмены ctx. Ошибки (брокер недоступен, база
// недоступна) не останавливают relay: события остаются в outbox и уйдут в
// следующем такте, а отставание видно в метрике orders_outbox_lag_seconds.
//
// После отмены ctx relay делает последний проход: события запросов,
// которые сервер дообработал при остановке, уходят сразу, а не после
// перезапуска.
func (r *Relay) Run(ctx context.Context) error {
	timer := time.NewTimer(0)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			r.drain(context.WithoutCancel(ctx))
			return nil
		case <-timer.C:
		}
		n, err := r.PublishOnce(ctx)
		if err != nil && ctx.Err() == nil {
			r.cfg.Logger.WarnContext(ctx, "outbox publish failed, will retry", "err", err)
		}
		if n == r.cfg.BatchSize {
			timer.Reset(0) // пачка полная — вероятно, ждут ещё
		} else {
			timer.Reset(r.cfg.Interval)
		}
	}
}

// drain публикует оставшееся при остановке — не дольше одного BatchTimeout
// на пачку и не больше десяти пачек.
func (r *Relay) drain(ctx context.Context) {
	for range 10 {
		n, err := r.PublishOnce(ctx)
		if err != nil {
			r.cfg.Logger.WarnContext(ctx, "outbox drain failed: events stay in outbox", "err", err)
			return
		}
		if n < r.cfg.BatchSize {
			return
		}
	}
}

// PublishOnce — один такт: пачка событий → Kafka → отметка. Отменённый ctx
// прерывает только ожидание между тактами (Run): сам такт работает со своим
// таймаутом, чтобы не бросить пачку на полпути при остановке.
func (r *Relay) PublishOnce(ctx context.Context) (int, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), r.cfg.BatchTimeout)
	defer cancel()
	n, err := r.store.PublishPending(ctx, r.cfg.BatchSize, r.publish)
	if err != nil {
		r.m.errors.Inc()
	} else {
		r.m.published.Add(float64(n))
	}
	if pending, oldest, serr := r.store.PendingStats(ctx); serr == nil {
		r.m.pending.Set(float64(pending))
		lag := 0.0
		if pending > 0 {
			lag = max(0, r.cfg.Now().Sub(oldest).Seconds())
		}
		r.m.lag.Set(lag)
	}
	return n, err
}

// Заголовки записи — часть контракта (см. events.proto).
const (
	HeaderEventID     = "event-id"
	HeaderEventType   = "event-type"
	HeaderContentType = "content-type"
)

// publish отправляет пачку и ждёт подтверждения всех записей.
//
// Продюсер franz-go по умолчанию идемпотентный (acks=all, номера
// последовательностей): повтор отправки внутри клиента не создаёт дублей
// в партиции и не меняет порядок. Дубли возможны только между тактами
// relay (отправили, не успели отметить), их отсекают потребители.
func (r *Relay) publish(ctx context.Context, events []orders.Event) error {
	records := make([]*kgo.Record, len(events))
	spans := make(map[*kgo.Record]trace.Span, len(events))
	prop := otel.GetTextMapPropagator()
	for i, ev := range events {
		// Родитель спана — запрос, создавший событие (контекст сохранён в
		// outbox): публикация видна в той же трассе, что и CreateOrder.
		parent := prop.Extract(ctx, propagation.MapCarrier(ev.TraceContext))
		spanCtx, span := r.tracer.Start(parent, r.cfg.Topic+" publish",
			trace.WithSpanKind(trace.SpanKindProducer),
			trace.WithAttributes(
				semconv.MessagingSystemKafka,
				semconv.MessagingDestinationName(r.cfg.Topic),
				semconv.MessagingOperationTypeSend,
				semconv.MessagingMessageID(ev.EventID),
				semconv.MessagingKafkaMessageKey(strconv.FormatInt(ev.OrderID, 10)),
			))
		rec := &kgo.Record{
			Topic: r.cfg.Topic,
			// Ключ — номер заказа: все события заказа попадают в одну
			// партицию и читаются в порядке записи.
			Key:   []byte(strconv.FormatInt(ev.OrderID, 10)),
			Value: ev.Payload,
			Headers: []kgo.RecordHeader{
				{Key: HeaderEventID, Value: []byte(ev.EventID)},
				{Key: HeaderEventType, Value: []byte(ev.Type)},
				{Key: HeaderContentType, Value: []byte("application/x-protobuf")},
			},
		}
		// В заголовки — контекст спана публикации: обработка у
		// потребителя станет его потомком.
		prop.Inject(spanCtx, kafkax.NewHeaderCarrier(rec))
		records[i] = rec
		spans[rec] = span
	}

	// ProduceSync возвращает результаты в порядке подтверждения, а не
	// отправки, — сопоставляем по указателю на запись.
	results := r.producer.ProduceSync(ctx, records...)
	var errs []error
	for _, res := range results {
		span := spans[res.Record]
		if span == nil {
			continue
		}
		if res.Err != nil {
			span.RecordError(res.Err)
			span.SetStatus(codes.Error, "produce failed")
			errs = append(errs, res.Err)
		} else {
			span.SetAttributes(
				semconv.MessagingDestinationPartitionID(strconv.Itoa(int(res.Record.Partition))),
				semconv.MessagingKafkaOffset(int(res.Record.Offset)),
			)
		}
		span.End()
		delete(spans, res.Record)
	}
	for _, span := range spans { // на случай, если результат не пришёл
		span.End()
	}
	if len(errs) > 0 {
		// Отметки нет ни у одного события пачки: уйдут снова все. Порядок
		// по ключу важнее, чем лишние дубли.
		return fmt.Errorf("produce %d of %d events: %w", len(errs), len(records), errors.Join(errs...))
	}
	return nil
}

type metrics struct {
	published prometheus.Counter
	errors    prometheus.Counter
	pending   prometheus.Gauge
	lag       prometheus.Gauge
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		published: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_outbox_published_total",
			Help: "Outbox events published to Kafka.",
		}),
		errors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_outbox_publish_errors_total",
			Help: "Failed outbox relay batches.",
		}),
		pending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "orders_outbox_pending_events",
			Help: "Outbox events waiting to be published.",
		}),
		lag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "orders_outbox_lag_seconds",
			Help: "Age of the oldest unpublished outbox event (0 when none).",
		}),
	}
	if reg != nil {
		reg.MustRegister(m.published, m.errors, m.pending, m.lag)
	}
	return m
}
