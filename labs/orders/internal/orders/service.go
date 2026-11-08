package orders

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"time"
)

// Лимиты входных данных: защищают базу от заказа на миллион позиций и
// заодно исключают переполнение суммы.
const (
	MaxItems    = 100
	MaxQuantity = 1000
)

// CreateOrderRequest — данные для создания заказа.
type CreateOrderRequest struct {
	CustomerID int64
	Items      []ItemRequest
}

// ItemRequest — позиция в запросе: какой товар и сколько.
type ItemRequest struct {
	ProductID int64
	Quantity  int
}

// Service — бизнес-операции с заказами. Зависимости передаются в
// конструктор явно, без DI-контейнера: граф собирается в main.
type Service struct {
	store Store
	now   func() time.Time
}

// NewService создаёт сервис. now — источник времени (в main — time.Now,
// в тестах — фиксированные часы).
func NewService(store Store, now func() time.Time) *Service {
	return &Service{
		store: store,
		// PostgreSQL и MySQL хранят время с точностью до микросекунд.
		// Обрезаем сразу, чтобы заказ, возвращённый из CreateOrder, совпадал
		// с прочитанным потом из базы. И всегда UTC.
		now: func() time.Time { return now().UTC().Truncate(time.Microsecond) },
	}
}

// CreateOrder создаёт заказ и резервирует товар — всё в одной транзакции:
// либо есть и заказ, и резерв, и событие в outbox, либо нет ничего.
func (s *Service) CreateOrder(ctx context.Context, req CreateOrderRequest) (Order, error) {
	if err := req.validate(); err != nil {
		return Order{}, err
	}

	// Позиции резервируем в порядке возрастания ProductID. Все транзакции
	// блокируют строки stock в одном порядке, поэтому два встречных заказа
	// (A, B) и (B, A) не попадут во взаимоблокировку (deadlock).
	items := slices.Clone(req.Items)
	slices.SortFunc(items, func(a, b ItemRequest) int { return cmp.Compare(a.ProductID, b.ProductID) })
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ProductID
	}
	// ProductID → индекс в исходном запросе: ошибка должна указывать на
	// позицию так, как её прислал клиент, а не после сортировки.
	position := make(map[int64]int, len(req.Items))
	for i, it := range req.Items {
		position[it.ProductID] = i
	}

	now := s.now()
	order := Order{
		CustomerID: req.CustomerID,
		Status:     StatusReserved,
		Items:      make([]Item, 0, len(items)),
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	err := s.inTx(ctx, func(tx Tx) error {
		// Все товары — одним запросом, а не по запросу на позицию (N+1).
		products, err := tx.ProductsByIDs(ctx, ids)
		if err != nil {
			return err
		}
		byID := make(map[int64]Product, len(products))
		for _, p := range products {
			byID[p.ID] = p
		}

		for _, it := range items {
			p, ok := byID[it.ProductID]
			if !ok {
				return &ValidationError{
					Field:   fmt.Sprintf("items[%d].product_id", position[it.ProductID]),
					Message: "product not found",
				}
			}
			// Остаток проверяет и списывает сама база — одной инструкцией
			// UPDATE … WHERE available >= qty. Схема «прочитал остаток,
			// сравнил в Go, записал» здесь была бы гонкой: два заказа
			// прочитали бы один и тот же остаток.
			if err := tx.ReserveStock(ctx, it.ProductID, it.Quantity); err != nil {
				return fmt.Errorf("reserve product %d: %w", it.ProductID, err)
			}
			order.Items = append(order.Items, Item{
				ProductID:  it.ProductID,
				Quantity:   it.Quantity,
				PriceCents: p.PriceCents,
			})
			order.TotalCents += p.PriceCents * int64(it.Quantity)
		}

		id, err := tx.CreateOrder(ctx, order)
		if err != nil {
			return err
		}
		order.ID = id

		return addOrderEvent(ctx, tx, EventOrderCreated, order)
	})
	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}
	return order, nil
}

// CancelOrder отменяет заказ и возвращает товар на склад.
//
// Повторная отмена уже отменённого заказа — не ошибка: метод возвращает
// заказ как есть и остатки второй раз не трогает. Клиент, у которого
// оборвался ответ, может безопасно повторить запрос.
func (s *Service) CancelOrder(ctx context.Context, id int64) (Order, error) {
	var order Order
	err := s.inTx(ctx, func(tx Tx) error {
		// Блокировка строки заказа: параллельная отмена того же заказа ждёт
		// здесь и после нашего коммита увидит уже StatusCancelled.
		o, err := tx.LockOrder(ctx, id)
		if err != nil {
			return err
		}
		if o.Status == StatusCancelled {
			order = o
			return nil
		}
		if !o.Status.CanTransitionTo(StatusCancelled) {
			return fmt.Errorf("order %d in status %q: %w", id, o.Status, ErrConflict)
		}

		now := s.now()
		// Условие на старый статус в UPDATE — страховка на случай, если
		// блокировки нет (или её забудут): 0 строк → ErrConflict.
		if err := tx.UpdateOrderStatus(ctx, id, o.Status, StatusCancelled, now); err != nil {
			return err
		}
		// Позиции упорядочены по ProductID — тот же порядок блокировок,
		// что и при создании заказа.
		for _, it := range o.Items {
			if err := tx.ReleaseStock(ctx, it.ProductID, it.Quantity); err != nil {
				return fmt.Errorf("release product %d: %w", it.ProductID, err)
			}
		}
		o.Status = StatusCancelled
		o.UpdatedAt = now
		order = o

		return addOrderEvent(ctx, tx, EventOrderCancelled, o)
	})
	if err != nil {
		return Order{}, fmt.Errorf("cancel order %d: %w", id, err)
	}
	return order, nil
}

// Order возвращает заказ с позициями.
func (s *Service) Order(ctx context.Context, id int64) (Order, error) {
	o, err := s.store.Order(ctx, id)
	if err != nil {
		return Order{}, fmt.Errorf("get order %d: %w", id, err)
	}
	return o, nil
}

// Product возвращает товар с текущим остатком.
func (s *Service) Product(ctx context.Context, id int64) (Product, error) {
	p, err := s.store.Product(ctx, id)
	if err != nil {
		return Product{}, fmt.Errorf("get product %d: %w", id, err)
	}
	return p, nil
}

// inTx выполняет fn в транзакции: Commit, если fn вернула nil, иначе
// Rollback. Это единственное место, где сервис открывает транзакции.
func (s *Service) inTx(ctx context.Context, fn func(tx Tx) error) error {
	tx, err := s.store.Begin(ctx)
	if err != nil {
		return err
	}
	// Откат в defer срабатывает при любом выходе — по ошибке, по панике.
	// После успешного Commit он ничего не делает, его ошибку не проверяем.
	defer tx.Rollback(ctx)

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// orderEvent — JSON событий order.*. Это контракт с потребителями из
// модуля 12 (через Kafka), поэтому отдельный тип с явными тегами, а не
// сериализация доменной структуры.
type orderEvent struct {
	OrderID    int64            `json:"order_id"`
	CustomerID int64            `json:"customer_id"`
	Status     string           `json:"status"`
	TotalCents int64            `json:"total_cents"`
	Items      []orderEventItem `json:"items"`
	OccurredAt time.Time        `json:"occurred_at"`
}

type orderEventItem struct {
	ProductID  int64 `json:"product_id"`
	Quantity   int   `json:"quantity"`
	PriceCents int64 `json:"price_cents"`
}

// addOrderEvent пишет событие о заказе в outbox в рамках транзакции tx.
func addOrderEvent(ctx context.Context, tx Tx, eventType string, o Order) error {
	ev := orderEvent{
		OrderID:    o.ID,
		CustomerID: o.CustomerID,
		Status:     string(o.Status),
		TotalCents: o.TotalCents,
		Items:      make([]orderEventItem, len(o.Items)),
		OccurredAt: o.UpdatedAt,
	}
	for i, it := range o.Items {
		ev.Items[i] = orderEventItem{ProductID: it.ProductID, Quantity: it.Quantity, PriceCents: it.PriceCents}
	}
	payload, err := json.Marshal(ev)
	if err != nil {
		return fmt.Errorf("marshal %s event: %w", eventType, err)
	}
	return tx.AddEvent(ctx, Event{
		Type:      eventType,
		OrderID:   o.ID,
		Payload:   payload,
		CreatedAt: o.UpdatedAt,
	})
}

// validate проверяет запрос до похода в базу. Возвращает первую найденную
// проблему: этого достаточно, чтобы клиент исправил запрос.
func (r CreateOrderRequest) validate() error {
	if r.CustomerID <= 0 {
		return &ValidationError{Field: "customer_id", Message: "must be positive"}
	}
	if len(r.Items) == 0 {
		return &ValidationError{Field: "items", Message: "must not be empty"}
	}
	if len(r.Items) > MaxItems {
		return &ValidationError{Field: "items", Message: fmt.Sprintf("must contain at most %d items", MaxItems)}
	}
	seen := make(map[int64]bool, len(r.Items))
	for i, it := range r.Items {
		switch {
		case it.ProductID <= 0:
			return &ValidationError{Field: fmt.Sprintf("items[%d].product_id", i), Message: "must be positive"}
		case it.Quantity < 1 || it.Quantity > MaxQuantity:
			return &ValidationError{
				Field:   fmt.Sprintf("items[%d].quantity", i),
				Message: fmt.Sprintf("must be between 1 and %d", MaxQuantity),
			}
		case seen[it.ProductID]:
			// Две строки с одним товаром — почти наверняка ошибка клиента;
			// молча складывать количества не будем.
			return &ValidationError{Field: fmt.Sprintf("items[%d].product_id", i), Message: "duplicate product"}
		}
		seen[it.ProductID] = true
	}
	return nil
}
