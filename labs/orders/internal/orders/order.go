// Package orders — домен сервиса «Заказы»: заказ, позиции, статусы, склад,
// доменные ошибки и сервис с бизнес-правилами.
//
// Пакет не знает ни про HTTP, ни про конкретную базу: хранилище он видит
// через интерфейс Store, объявленный здесь же, на стороне потребителя
// (в C# интерфейс обычно лежит рядом с реализацией — в Go наоборот).
// Доменные типы без JSON-тегов: формат API — забота пакета httpapi.
package orders

import "time"

// Status — статус заказа. Строка, а не int: её видно в базе и в логах.
type Status string

const (
	// StatusNew — заказ принят, резерв ещё не подтверждён. Сейчас заказ и
	// резерв создаются в одной транзакции, и заказ сразу получает
	// StatusReserved. В модуле 11 резерв уйдёт в сервис «Склад» (saga),
	// и этот статус станет видимым.
	StatusNew Status = "new"
	// StatusReserved — товар зарезервирован на складе.
	StatusReserved Status = "reserved"
	// StatusCancelled — заказ отменён, резерв возвращён на склад. Конечный статус.
	StatusCancelled Status = "cancelled"
)

// transitions — допустимые переходы. Таблица, а не цепочка if: новый
// статус (оплачен, отгружен) добавляется одной строкой.
var transitions = map[Status][]Status{
	StatusNew:      {StatusReserved, StatusCancelled},
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

// Product — товар каталога с текущим остатком на складе.
type Product struct {
	ID         int64
	Name       string
	PriceCents int64
	Available  int
}

// NewProduct — данные для добавления товара в каталог.
type NewProduct struct {
	Name       string
	PriceCents int64
	Available  int
}

// Типы событий outbox. Имена — часть контракта с потребителями (модуль 12):
// меняются только с версионированием.
const (
	EventOrderCreated   = "order.created"
	EventOrderCancelled = "order.cancelled"
)

// Event — событие в таблице outbox. Payload — готовый JSON: публикатор
// отправит его в Kafka как есть.
type Event struct {
	ID        int64 // присваивает база
	Type      string
	OrderID   int64
	Payload   []byte
	CreatedAt time.Time
}
