// Package consumer — цикл чтения событий заказов из Kafka на franz-go:
// consumer group, ручной коммит офсетов после обработки, повторы, DLQ
// (dead letter topic), трассировка и метрики.
//
// В .NET то же обычно пишут на Confluent.Kafka (Consume в цикле
// BackgroundService, EnableAutoCommit=false, StoreOffset/Commit) или берут
// MassTransit/Rider. Здесь — один явный цикл: всё, что происходит с записью
// между «прочитали» и «закоммитили», видно в одном файле.
//
// Гарантия — at-least-once: офсет коммитится только после того, как запись
// обработана или надёжно лежит в DLQ. Сбой между обработкой и коммитом
// приводит к повторной доставке — её отсеивает идемпотентность
// обработчика (processed_events). Exactly-once для побочных эффектов вне
// Kafka не бывает; бывает at-least-once плюс дедупликация.
package consumer

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"

	"gotutor/labs/notifications/internal/notify"
	"gotutor/labs/platform/kafkax"
	"gotutor/labs/platform/resilience"
)

// ClientOptions — настройки kgo.Client, без которых Consumer работает
// неправильно. main и тесты добавляют к ним адреса брокеров.
func ClientOptions(topic, group string) []kgo.Opt {
	return []kgo.Opt{
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topic),
		// Новая группа читает топик с начала: события, опубликованные до
		// первого запуска «Уведомлений», тоже должны дойти (по умолчанию
		// так и есть; здесь — явно, как auto.offset.reset=earliest).
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
		// Автокоммит franz-go коммитит то, что уже выдано опросом, по
		// таймеру — то есть, возможно, ещё не обработанное. Падение между
		// коммитом и обработкой теряет запись. Поэтому коммитим сами.
		kgo.DisableAutoCommit(),
		// Пока пачка у нас в руках, группа не может отобрать партиции
		// (ребалансировка ждёт AllowRebalance). Иначе можно закоммитить
		// офсет партиции, которая уже принадлежит другому участнику, и
		// откатить его коммит назад. Цена — пачку надо обрабатывать
		// быстрее, чем rebalance timeout (по умолчанию 60 с), поэтому она
		// ограничена MaxPollRecords.
		kgo.BlockRebalanceOnPoll(),
		// Отправка в DLQ: одна попытка ProduceSync не дольше этого времени,
		// дальше — наш повтор с журналом (иначе при недоступном брокере
		// ProduceSync молча висел бы до отмены контекста).
		kgo.RecordDeliveryTimeout(10 * time.Second),
	}
}

// Config — настройки консьюмера. Нулевые значения заменяются значениями по
// умолчанию (см. New).
type Config struct {
	// Group — consumer group; попадает в спан и заголовки DLQ.
	Group string
	// DLQTopic — топик для записей, которые не удалось обработать.
	DLQTopic string
	// MaxAttempts — попыток обработки записи, включая первую (по умолчанию 3).
	MaxAttempts int
	// RetryBaseDelay и RetryMaxDelay — паузы между попытками обработки
	// (по умолчанию 100 мс и 2 с, экспонента с jitter).
	RetryBaseDelay, RetryMaxDelay time.Duration
	// DLQBaseDelay и DLQMaxDelay — паузы между попытками отправки в DLQ
	// (по умолчанию 200 мс и 10 с). Попыток — до отмены.
	DLQBaseDelay, DLQMaxDelay time.Duration
	// MaxPollRecords — записей в пачке (по умолчанию 100).
	MaxPollRecords int
	// FinishTimeout — сколько после сигнала остановки даётся на
	// дообработку текущей пачки (по умолчанию 10 с).
	FinishTimeout time.Duration
	// CommitTimeout — на коммит офсетов пачки (по умолчанию 10 с).
	CommitTimeout time.Duration

	// Sleep ждёт d или отмены ctx; nil — настоящий таймер. Тесты
	// подставляют мгновенный сон.
	Sleep func(ctx context.Context, d time.Duration) error
	// TracerProvider и Propagator; nil — глобальные из otel (их настраивает
	// telemetry.Setup). Тесты передают свои, не трогая глобальное состояние.
	TracerProvider trace.TracerProvider
	Propagator     propagation.TextMapPropagator
}

// Consumer читает записи, обрабатывает их обработчиком notify и коммитит
// офсеты.
type Consumer struct {
	cl      *kgo.Client
	handler *notify.Handler
	cfg     Config
	log     *slog.Logger
	m       *Metrics
	tracer  trace.Tracer
	prop    propagation.TextMapPropagator
}

// New собирает консьюмер. Клиент cl создан с ClientOptions; им же
// отправляются записи в DLQ. Закрывает клиент вызывающий — после Run.
func New(cl *kgo.Client, h *notify.Handler, cfg Config, log *slog.Logger, m *Metrics) *Consumer {
	setDefault(&cfg.MaxAttempts, 3)
	setDefault(&cfg.RetryBaseDelay, 100*time.Millisecond)
	setDefault(&cfg.RetryMaxDelay, 2*time.Second)
	setDefault(&cfg.DLQBaseDelay, 200*time.Millisecond)
	setDefault(&cfg.DLQMaxDelay, 10*time.Second)
	setDefault(&cfg.MaxPollRecords, 100)
	setDefault(&cfg.FinishTimeout, 10*time.Second)
	setDefault(&cfg.CommitTimeout, 10*time.Second)
	tp := cfg.TracerProvider
	if tp == nil {
		tp = otel.GetTracerProvider()
	}
	prop := cfg.Propagator
	if prop == nil {
		prop = otel.GetTextMapPropagator()
	}
	return &Consumer{
		cl:      cl,
		handler: h,
		cfg:     cfg,
		log:     log,
		m:       m,
		tracer:  tp.Tracer("gotutor/labs/notifications/internal/consumer"),
		prop:    prop,
	}
}

func setDefault[T comparable](v *T, def T) {
	var zero T
	if *v == zero {
		*v = def
	}
}

// ErrClientClosed — клиент Kafka закрыт, пока Run ещё работал.
var ErrClientClosed = errors.New("kafka client closed")

// Run читает и обрабатывает записи до отмены ctx. После отмены перестаёт
// опрашивать, дообрабатывает текущую пачку (не дольше FinishTimeout),
// коммитит её и возвращает nil. Группу Run не покидает: вызывающий после
// него вызывает cl.CloseAllowingRebalance().
//
// Если топика ещё нет, опрос просто ждёт: franz-go перечитывает метаданные
// и начнёт читать, когда «Заказы» создадут топик.
func (c *Consumer) Run(ctx context.Context) error {
	c.log.InfoContext(ctx, "consumer started", "group", c.cfg.Group, "dlq_topic", c.cfg.DLQTopic)
	for {
		fetches := c.cl.PollRecords(ctx, c.cfg.MaxPollRecords)
		if fetches.IsClientClosed() {
			return ErrClientClosed
		}
		fetches.EachError(func(topic string, partition int32, err error) {
			// Отмена ctx приходит в опрос «поддельной» ошибкой — это
			// штатная остановка, не сбой.
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return
			}
			c.log.WarnContext(ctx, "fetch error", "topic", topic, "partition", partition, "err", err)
		})
		c.processBatch(ctx, fetches)
		// Пачка обработана и закоммичена — группа может перераспределять
		// партиции.
		c.cl.AllowRebalance()
		if ctx.Err() != nil {
			c.log.InfoContext(ctx, "consumer stopped")
			return nil
		}
	}
}

// processBatch обрабатывает записи пачки по порядку внутри каждой партиции
// и коммитит обработанные.
func (c *Consumer) processBatch(ctx context.Context, fetches kgo.Fetches) {
	if fetches.Empty() {
		return
	}
	// Сигнал остановки не прерывает пачку на середине: её контекст не
	// наследует отмену ctx (context.WithoutCancel, как ShutdownAsync в
	// .NET получает свой токен), а отменяется через FinishTimeout после
	// неё.
	bctx, cancel := withGrace(ctx, c.cfg.FinishTimeout)
	defer cancel()

	var done []*kgo.Record
	fetches.EachPartition(func(p kgo.FetchTopicPartition) {
		for _, r := range p.Records {
			if !c.handleRecord(bctx, r) {
				// Запись не обработана (истекло время остановки): ни её, ни
				// следующие записи партиции коммитить нельзя — их прочитает
				// следующий владелец партиции.
				return
			}
			done = append(done, r)
		}
	})
	if len(done) == 0 {
		return
	}

	// Коммит — со своим таймаутом и тоже без отмены сигналом: обработанное
	// стоит зафиксировать, иначе после перезапуска пачка придёт снова.
	cctx, ccancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.CommitTimeout)
	defer ccancel()
	// CommitRecords коммитит по каждой партиции наибольший офсет + 1.
	// Записи партиции обработаны подряд с начала пачки, поэтому дыр нет.
	if err := c.cl.CommitRecords(cctx, done...); err != nil {
		// Не фатально: записи придут ещё раз и будут отсеяны как повторы.
		c.log.WarnContext(ctx, "commit failed, records will be redelivered", "records", len(done), "err", err)
	}
}

// errFinishTimeout — причина отмены контекста пачки.
var errFinishTimeout = errors.New("finish timeout exceeded on shutdown")

// withGrace возвращает контекст, который сохраняет значения parent, но
// отменяется не вместе с ним, а через grace после его отмены.
func withGrace(parent context.Context, grace time.Duration) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(context.WithoutCancel(parent))
	stop := context.AfterFunc(parent, func() {
		t := time.NewTimer(grace)
		defer t.Stop()
		select {
		case <-t.C:
			cancel(errFinishTimeout)
		case <-ctx.Done():
		}
	})
	return ctx, func() {
		stop()
		cancel(nil)
	}
}

// handleRecord обрабатывает одну запись. true — запись обработана или
// лежит в DLQ, её офсет можно коммитить; false — ctx отменён раньше.
func (c *Consumer) handleRecord(ctx context.Context, r *kgo.Record) bool {
	start := time.Now()
	defer func() { c.m.duration.Observe(time.Since(start).Seconds()) }()

	// Продолжаем трассу продюсера: traceparent из заголовков записи. Спан
	// обработки — потомок спана публикации в relay «Заказов», поэтому в
	// Jaeger видна вся цепочка от HTTP-запроса создания заказа.
	ctx = c.prop.Extract(ctx, kafkax.NewHeaderCarrier(r))
	ctx, span := c.tracer.Start(ctx, r.Topic+" process",
		trace.WithSpanKind(trace.SpanKindConsumer),
		trace.WithAttributes(
			semconv.MessagingSystemKafka,
			semconv.MessagingOperationTypeProcess,
			semconv.MessagingOperationName("process"),
			semconv.MessagingDestinationName(r.Topic),
			semconv.MessagingDestinationPartitionID(strconv.Itoa(int(r.Partition))),
			semconv.MessagingKafkaOffset(int(r.Offset)),
			semconv.MessagingConsumerGroupName(c.cfg.Group),
			semconv.MessagingMessageBodySize(len(r.Value)),
		))
	defer span.End()
	log := c.log.With("topic", r.Topic, "partition", r.Partition, "offset", r.Offset)

	ev, err := notify.Decode(r.Value)
	if err != nil {
		// «Ядовитое» сообщение: повтор ничего не изменит, а застрявшая
		// запись остановила бы всю партицию. Сразу в DLQ.
		span.RecordError(err)
		span.SetStatus(codes.Error, "decode failed")
		log.WarnContext(ctx, "cannot decode record", "err", err)
		if c.sendToDLQ(ctx, log, r, reasonDecode, err, 1) != nil {
			return false
		}
		c.m.events.WithLabelValues(notify.EventUnknown, resultDLQ).Inc()
		return true
	}
	span.SetAttributes(semconv.MessagingMessageID(ev.GetEventId()))
	log = log.With("event_id", ev.GetEventId())

	var (
		out      notify.Outcome
		attempts int
	)
	retry := resilience.Retry{
		MaxAttempts: c.cfg.MaxAttempts,
		BaseDelay:   c.cfg.RetryBaseDelay,
		MaxDelay:    c.cfg.RetryMaxDelay,
		// Повторяем любую ошибку хранилища (база занята, диск, таймаут),
		// кроме отмены: её причина — остановка, а не запись.
		Retryable: func(err error) bool {
			return !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
		},
		Sleep: c.cfg.Sleep,
		OnRetry: func(attempt int, delay time.Duration, err error) {
			c.m.retries.Inc()
			log.WarnContext(ctx, "processing failed, retrying", "attempt", attempt, "delay", delay, "err", err)
		},
	}
	err = retry.Do(ctx, func(ctx context.Context) error {
		attempts++
		var err error
		out, err = c.handler.Handle(ctx, ev)
		return err
	})
	if err != nil {
		if ctx.Err() != nil {
			// Не успели до конца остановки: запись не наша вина, в DLQ её
			// не кладём — её прочитают заново после перезапуска.
			log.WarnContext(ctx, "processing interrupted by shutdown", "err", err)
			return false
		}
		span.RecordError(err)
		span.SetStatus(codes.Error, "processing failed")
		log.ErrorContext(ctx, "processing failed, sending to dlq", "attempts", attempts, "err", err)
		if c.sendToDLQ(ctx, log, r, reasonProcessing, err, attempts) != nil {
			return false
		}
		c.m.events.WithLabelValues(notify.EventType(ev), resultDLQ).Inc()
		return true
	}

	c.m.events.WithLabelValues(out.EventType, string(out.Result)).Inc()
	if out.Result == notify.ResultSent {
		c.m.sent.WithLabelValues(string(out.Kind)).Inc()
	}
	return true
}

// sendToDLQ кладёт копию записи в DLQ и ждёт подтверждения брокера. Пока
// подтверждения нет, офсет коммитить нельзя — иначе запись потеряна.
// Поэтому при недоступном DLQ попытки идут до отмены ctx (на остановке —
// FinishTimeout): консьюмер стоит, но ничего не теряет и не пропускает.
func (c *Consumer) sendToDLQ(ctx context.Context, log *slog.Logger, r *kgo.Record, reason string, cause error, attempts int) error {
	retry := resilience.Retry{
		MaxAttempts: math.MaxInt,
		BaseDelay:   c.cfg.DLQBaseDelay,
		MaxDelay:    c.cfg.DLQMaxDelay,
		Retryable:   func(error) bool { return true },
		Sleep:       c.cfg.Sleep,
		OnRetry: func(attempt int, delay time.Duration, err error) {
			log.WarnContext(ctx, "dlq produce failed, retrying", "attempt", attempt, "delay", delay, "err", err)
		},
	}
	err := retry.Do(ctx, func(ctx context.Context) error {
		// Новая запись на каждую попытку: ProduceSync заполняет у записи
		// партицию, офсет и время, повторно её не используют.
		return c.cl.ProduceSync(ctx, c.dlqRecord(r, reason, cause, attempts)).FirstErr()
	})
	if err != nil {
		log.ErrorContext(ctx, "dlq produce aborted", "reason", reason, "err", err)
		return fmt.Errorf("produce to dlq: %w", err)
	}
	c.m.dlq.WithLabelValues(reason).Inc()
	log.WarnContext(ctx, "record sent to dlq", "reason", reason, "attempts", attempts)
	return nil
}

// Заголовки записи в DLQ сверх заголовков оригинала.
const (
	HeaderDLQReason          = "dlq-reason"
	HeaderDLQError           = "dlq-error"
	HeaderDLQAttempts        = "dlq-attempts"
	HeaderDLQSourceTopic     = "dlq-source-topic"
	HeaderDLQSourcePartition = "dlq-source-partition"
	HeaderDLQSourceOffset    = "dlq-source-offset"
	HeaderDLQConsumerGroup   = "dlq-consumer-group"
)

// maxDLQErrorLen ограничивает текст ошибки в заголовке: заголовки едут с
// каждой записью и не должны раздувать её.
const maxDLQErrorLen = 256

// dlqRecord — копия записи для DLQ: те же ключ и значение (по ним запись
// можно разобрать и переотправить в исходный топик после исправления),
// заголовки оригинала (event-id, traceparent…) плюс сведения о сбое.
func (c *Consumer) dlqRecord(r *kgo.Record, reason string, cause error, attempts int) *kgo.Record {
	out := &kgo.Record{
		Topic:   c.cfg.DLQTopic,
		Key:     r.Key,
		Value:   r.Value,
		Headers: append([]kgo.RecordHeader(nil), r.Headers...),
	}
	// SetHeader заменяет, а не дублирует: запись, которая уже побывала в
	// DLQ и вернулась, не накопит два dlq-reason.
	kafkax.SetHeader(out, HeaderDLQReason, reason)
	kafkax.SetHeader(out, HeaderDLQError, truncate(cause.Error(), maxDLQErrorLen))
	kafkax.SetHeader(out, HeaderDLQAttempts, strconv.Itoa(attempts))
	kafkax.SetHeader(out, HeaderDLQSourceTopic, r.Topic)
	kafkax.SetHeader(out, HeaderDLQSourcePartition, strconv.Itoa(int(r.Partition)))
	kafkax.SetHeader(out, HeaderDLQSourceOffset, strconv.FormatInt(r.Offset, 10))
	kafkax.SetHeader(out, HeaderDLQConsumerGroup, c.cfg.Group)
	return out
}

// truncate обрезает s до n байт, не разрывая символ UTF-8.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}
