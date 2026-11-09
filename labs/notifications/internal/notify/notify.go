// Package notify — предметная логика «Уведомлений»: разобрать событие заказа,
// отсеять повтор, сохранить уведомление и «отправить» его покупателю.
//
// Пакет ничего не знает о Kafka: на вход — байты значения записи и
// разобранное событие, на выход — итог обработки. Повторы, DLQ, коммит
// офсетов и трассировка — забота пакета consumer. Так обработчик
// тестируется без брокера, а консьюмер — без базы (через этот обработчик
// с подменённым хранилищем).
package notify

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"google.golang.org/protobuf/proto"

	eventsv1 "gotutor/labs/api/gen/events/v1"
)

// Kind — вид уведомления. Значения совпадают с CHECK в схеме и метками
// метрик.
type Kind string

const (
	KindOrderCreated   Kind = "order_created"
	KindOrderCancelled Kind = "order_cancelled"
)

// Notification — уведомление покупателю.
type Notification struct {
	// EventID — id события, из которого уведомление получено; по нему
	// хранилище отсеивает повторы.
	EventID    string
	OrderID    int64
	CustomerID int64
	Kind       Kind
	Text       string
	CreatedAt  time.Time
}

// Store — то, что обработчику нужно от хранилища. Интерфейс объявлен здесь,
// у потребителя, а не рядом с реализацией: в Go интерфейсы реализуются
// неявно, и пакету sqlite не нужно знать о notify (в отличие от C#, где
// класс перечисляет интерфейсы в объявлении).
type Store interface {
	// SaveOnce в одной транзакции отмечает событие n.EventID обработанным и
	// сохраняет n. false без ошибки — событие уже было обработано (повтор),
	// n не сохранено.
	SaveOnce(ctx context.Context, n Notification) (bool, error)
}

// ErrInvalidEvent — значение записи не является корректным OrderEvent.
// Повторять обработку такой записи бессмысленно: она «ядовитая» и уходит
// в DLQ сразу.
var ErrInvalidEvent = errors.New("invalid event")

// Decode разбирает значение записи Kafka — OrderEvent в бинарном protobuf.
//
// Событие без event_id тоже считается некорректным: без него нельзя
// отсеять повтор, а контракт обещает id у каждого события.
func Decode(value []byte) (*eventsv1.OrderEvent, error) {
	ev := new(eventsv1.OrderEvent)
	if err := proto.Unmarshal(value, ev); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidEvent, err)
	}
	if ev.GetEventId() == "" {
		return nil, fmt.Errorf("%w: empty event_id", ErrInvalidEvent)
	}
	return ev, nil
}

// Тип события для метрик: ограниченный набор значений метки.
const (
	EventOrderCreated   = "order_created"
	EventOrderCancelled = "order_cancelled"
	EventUnknown        = "unknown"
)

// EventType возвращает тип события для метрик; nil — EventUnknown.
func EventType(ev *eventsv1.OrderEvent) string {
	switch ev.GetEvent().(type) {
	case *eventsv1.OrderEvent_OrderCreated:
		return EventOrderCreated
	case *eventsv1.OrderEvent_OrderCancelled:
		return EventOrderCancelled
	}
	return EventUnknown
}

// Result — чем закончилась обработка события.
type Result string

const (
	ResultSent      Result = "sent"      // уведомление сохранено и отправлено
	ResultDuplicate Result = "duplicate" // событие уже обрабатывалось
	ResultSkipped   Result = "skipped"   // незнакомый вариант oneof
)

// Outcome — итог обработки для метрик и журнала.
type Outcome struct {
	EventType string // EventOrderCreated, EventOrderCancelled или EventUnknown
	Result    Result
	Kind      Kind // только для ResultSent и ResultDuplicate
}

// Handler обрабатывает разобранные события. Безопасен для конкурентного
// использования, если таково хранилище.
type Handler struct {
	store Store
	now   func() time.Time
	log   *slog.Logger
}

// NewHandler возвращает обработчик. now внедряется, чтобы тесты знали время
// создания уведомления.
func NewHandler(store Store, now func() time.Time, log *slog.Logger) *Handler {
	return &Handler{store: store, now: now, log: log}
}

// Handle обрабатывает событие: сохраняет уведомление, если событие новое, и
// «отправляет» его. Ошибка — только от хранилища: её можно повторить.
//
// Порядок «сначала коммит, потом отправка» выбран сознательно. Таблица
// notifications — источник истины: что в ней есть, то покупателю и должно
// уйти. Отправка (здесь — запись в журнал, в жизни — письмо или push)
// стоит после коммита: если транзакция не прошла, отправлять нечего, и
// повтор обработки не пришлёт письмо дважды из-за отката. Обратный порядок
// («отправил, потом записал») при сбое записи слал бы письмо на каждую
// попытку. Остаётся окно «закоммитили, но упали до отправки» — поэтому в
// настоящей системе отправку делает отдельный воркер по таблице (тот же
// outbox, что у «Заказов»), а сам побочный эффект — тоже at-least-once:
// провайдер писем получает ключ идемпотентности (id уведомления).
func (h *Handler) Handle(ctx context.Context, ev *eventsv1.OrderEvent) (Outcome, error) {
	n, ok := h.notification(ev)
	if !ok {
		// Контракт: незнакомый вариант oneof (событие новой версии) —
		// не ошибка. Пропускаем и коммитим офсет.
		h.log.DebugContext(ctx, "unknown event skipped", "event_id", ev.GetEventId(), "order_id", ev.GetOrderId())
		return Outcome{EventType: EventUnknown, Result: ResultSkipped}, nil
	}
	out := Outcome{EventType: EventType(ev), Kind: n.Kind}

	saved, err := h.store.SaveOnce(ctx, n)
	if err != nil {
		return out, fmt.Errorf("save notification: %w", err)
	}
	if !saved {
		h.log.InfoContext(ctx, "duplicate event skipped", "event_id", n.EventID, "order_id", n.OrderID)
		out.Result = ResultDuplicate
		return out, nil
	}

	h.log.InfoContext(ctx, "notification sent",
		"event_id", n.EventID,
		"order_id", n.OrderID,
		"customer_id", n.CustomerID,
		"kind", string(n.Kind),
		"text", n.Text,
	)
	out.Result = ResultSent
	return out, nil
}

// notification строит уведомление по событию; false — вариант oneof
// незнаком.
func (h *Handler) notification(ev *eventsv1.OrderEvent) (Notification, bool) {
	n := Notification{
		EventID:   ev.GetEventId(),
		OrderID:   ev.GetOrderId(),
		CreatedAt: h.now().UTC(),
	}
	switch e := ev.GetEvent().(type) {
	case *eventsv1.OrderEvent_OrderCreated:
		n.Kind = KindOrderCreated
		n.CustomerID = e.OrderCreated.GetCustomerId()
		n.Text = fmt.Sprintf("Заказ %d оформлен на %s", n.OrderID, FormatRubles(e.OrderCreated.GetTotalCents()))
	case *eventsv1.OrderEvent_OrderCancelled:
		n.Kind = KindOrderCancelled
		n.CustomerID = e.OrderCancelled.GetCustomerId()
		n.Text = fmt.Sprintf("Заказ %d отменён", n.OrderID)
	default:
		return Notification{}, false
	}
	return n, true
}

// FormatRubles форматирует сумму в копейках по-русски: 598000 → «5 980,00 ₽».
//
// Разряды разделены обычным пробелом, а не неразрывным (U+00A0), как
// требует типографика: текст попадает в JSON-журнал, и его удобно искать
// grep'ом. В .NET то же дало бы ToString("N2", new CultureInfo("ru-RU")),
// но культура там вставляет U+00A0. Своя функция на десять строк — дешевле,
// чем golang.org/x/text/message ради одного формата.
func FormatRubles(cents int64) string {
	sign := ""
	// Через uint64: -math.MinInt64 не помещается в int64.
	u := uint64(cents)
	if cents < 0 {
		sign = "-"
		u = -u
	}
	digits := strconv.FormatUint(u/100, 10)
	var b []byte
	for i := range len(digits) {
		if i > 0 && (len(digits)-i)%3 == 0 {
			b = append(b, ' ')
		}
		b = append(b, digits[i])
	}
	return fmt.Sprintf("%s%s,%02d ₽", sign, b, u%100)
}
