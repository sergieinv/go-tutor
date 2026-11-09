// Package orders — домен сервиса «Заказы»: заказ, позиции, статусы,
// каталог, доменные ошибки и сервис с бизнес-правилами, в том числе сага
// создания заказа (резерв на складе → запись заказа → компенсация).
//
// Пакет не знает ни про gRPC, ни про HTTP, ни про конкретную базу или
// Kafka: хранилище и склад он видит через интерфейсы Store и Warehouse,
// объявленные здесь же, на стороне потребителя (в C# интерфейс обычно
// лежит рядом с реализацией — в Go наоборот).
package orders

import "time"

// Status — статус заказа. Строка, а не int: её видно в базе и в логах.
type Status string

const (
	// StatusReserved — товар зарезервирован на складе. Заказ записывается
	// только после успешного резерва, поэтому заказов «без резерва» в базе
	// не бывает. (В модуле 10 был ещё статус new: резерв и заказ делались
	// в одной транзакции одной базы. Теперь резерв — в другом сервисе, и
	// промежуточное состояние живёт только в памяти саги.)
	StatusReserved Status = "reserved"
	// StatusCancelled — заказ отменён, резерв возвращён. Конечный статус.
	StatusCancelled Status = "cancelled"
)

// transitions — допустимые переходы. Таблица, а не цепочка if: новый
// статус (оплачен, отгружен) добавляется одной строкой.
var transitions = map[Status][]Status{
	StatusReserved: {StatusCancelled},
}

// CanTransitionTo сообщает, можно ли перевести заказ из s в next.
func (s Status) CanTransitionTo(next Status) bool {
	for _, allowed := range transitions[s] {
		if allowed == next {
			return true
		}
	}
	return false
}

// Order — заказ вместе с позициями.
type Order struct {
	ID         int64
	CustomerID int64
	Status     Status
	Items      []Item // упорядочены по ProductID
	TotalCents int64
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

// Item — позиция заказа. Цена фиксируется на момент заказа.
type Item struct {
	ProductID  int64
	Quantity   int
	PriceCents int64
}

// Product — товар каталога. Каталог и цены — у «Заказов», остатки — у
// «Склада»: каждый сервис владеет своими данными, общих таблиц нет.
type Product struct {
	ID         int64
	Name       string
	PriceCents int64
}

// NewProduct — данные для добавления товара в каталог.
type NewProduct struct {
	Name       string
	PriceCents int64
}

// Типы событий — полные имена сообщений protobuf из events.v1 (заголовок
// event-type записи Kafka). Это часть контракта с потребителями.
const (
	EventOrderCreated   = "events.v1.OrderCreated"
	EventOrderCancelled = "events.v1.OrderCancelled"
)

// Event — событие в таблице outbox.
type Event struct {
	// ID — номер строки outbox (присваивает база); задаёт порядок публикации.
	ID int64
	// EventID — глобально уникальный id события: по нему потребители
	// отбрасывают дубли (доставка at-least-once).
	EventID string
	Type    string
	OrderID int64
	// Payload — events.v1.OrderEvent в бинарном protobuf; relay отправит
	// его в Kafka как есть.
	Payload []byte
	// TraceContext — контекст трассы запроса, создавшего событие
	// (traceparent/tracestate). Relay публикует событие позже, из другой
	// горутины, и продолжает по нему ту же трассу.
	TraceContext map[string]string
	CreatedAt    time.Time
}
