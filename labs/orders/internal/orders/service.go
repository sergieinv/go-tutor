package orders

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"time"
)

// Лимиты входных данных. Те же правила записаны в контракте (protovalidate
// в orders.proto) и проверяются interceptor'ом до вызова сервиса, но домен
// не полагается на транспорт: сервис можно вызвать и не через gRPC.
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

// Options — необязательные зависимости сервиса.
type Options struct {
	// Now — источник времени; nil — time.Now. В тестах — фиксированные часы.
	Now func() time.Time
	// Log — журнал; nil — в никуда.
	Log *slog.Logger
	// Observer — метрики саги; nil — не сообщать.
	Observer Observer
	// ReleaseTimeout — сколько ждать возврата резерва (компенсации или
	// отмены), если клиент уже ушёл; 0 — 10 секунд.
	ReleaseTimeout time.Duration
}

// Service — бизнес-операции с заказами. Зависимости передаются в
// конструктор явно, без DI-контейнера: граф собирается в main.
type Service struct {
	store          Store
	warehouse      Warehouse
	now            func() time.Time
	log            *slog.Logger
	obs            Observer
	releaseTimeout time.Duration
}

// NewService создаёт сервис.
func NewService(store Store, warehouse Warehouse, opts Options) *Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	log := opts.Log
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	obs := opts.Observer
	if obs == nil {
		obs = nopObserver{}
	}
	timeout := opts.ReleaseTimeout
	if timeout == 0 {
		timeout = 10 * time.Second
	}
	return &Service{
		store:     store,
		warehouse: warehouse,
		// PostgreSQL хранит время с точностью до микросекунд. Обрезаем
		// сразу, чтобы заказ, возвращённый из CreateOrder, совпадал с
		// прочитанным потом из базы. И всегда UTC.
		now:            func() time.Time { return now().UTC().Truncate(time.Microsecond) },
		log:            log,
		obs:            obs,
		releaseTimeout: timeout,
	}
}

type nopObserver struct{}

func (nopObserver) Released(string, error) {}

// CreateOrder создаёт заказ — сага из двух шагов с компенсацией:
//
//  1. Резерв на складе (gRPC WarehouseService.Reserve) с номером заказа как
//     ключом идемпотентности.
//  2. Транзакция в своей базе: заказ, позиции и событие OrderCreated в
//     outbox — вместе или ничего.
//
// Если шаг 2 не удался, шаг 1 отменяется компенсирующим действием —
// Release. Распределённой транзакции (двухфазного коммита) между базами
// разных сервисов нет: вместо атомарности — последовательность шагов,
// каждый из которых можно безопасно повторить или отменить.
func (s *Service) CreateOrder(ctx context.Context, req CreateOrderRequest) (Order, error) {
	if err := req.validate(); err != nil {
		return Order{}, err
	}
	items := slices.Clone(req.Items)
	slices.SortFunc(items, func(a, b ItemRequest) int { return cmp.Compare(a.ProductID, b.ProductID) })
	ids := make([]int64, len(items))
	for i, it := range items {
		ids[i] = it.ProductID
	}

	// Цены — из своего каталога, одним запросом (без N+1).
	products, err := s.store.ProductsByIDs(ctx, ids)
	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}
	byID := make(map[int64]Product, len(products))
	for _, p := range products {
		byID[p.ID] = p
	}
	now := s.now()
	order := Order{
		CustomerID: req.CustomerID,
		Status:     StatusReserved,
		Items:      make([]Item, 0, len(items)),
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	for _, it := range items {
		p, ok := byID[it.ProductID]
		if !ok {
			// Ошибка указывает на позицию так, как её прислал клиент.
			return Order{}, &ValidationError{
				Field:   fmt.Sprintf("items[%d].product_id", slices.IndexFunc(req.Items, func(r ItemRequest) bool { return r.ProductID == it.ProductID })),
				Message: "product not found",
			}
		}
		order.Items = append(order.Items, Item{ProductID: it.ProductID, Quantity: it.Quantity, PriceCents: p.PriceCents})
		order.TotalCents += p.PriceCents * int64(it.Quantity)
	}

	// Номер заказа — до резерва: это ключ идемпотентности для склада.
	order.ID, err = s.store.NextOrderID(ctx)
	if err != nil {
		return Order{}, fmt.Errorf("create order: %w", err)
	}

	// Шаг 1: резерв.
	if err := s.warehouse.Reserve(ctx, order.ID, items); err != nil {
		// «Не хватает товара» — точный ответ: резерва нет, отменять нечего.
		// Любая другая ошибка (таймаут, обрыв, недоступность) оставляет
		// исход неизвестным: склад мог успеть зарезервировать, а ответ
		// потерялся. Release идемпотентен и оставляет «надгробие», поэтому
		// безопасен и в том случае, если резерва нет или он ещё «в пути».
		if !errors.Is(err, ErrOutOfStock) {
			s.release(ctx, order.ID, ReleaseReasonReserveFailed)
		}
		return Order{}, fmt.Errorf("create order %d: reserve: %w", order.ID, err)
	}

	// Шаг 2: заказ и событие в своей базе.
	err = s.inTx(ctx, func(tx Tx) error {
		if err := tx.CreateOrder(ctx, order); err != nil {
			return err
		}
		return s.addEvent(ctx, tx, order)
	})
	if err == nil {
		return order, nil
	}

	var ce *commitError
	if errors.As(err, &ce) {
		// Ошибка на COMMIT неоднозначна: соединение оборвалось, а база,
		// возможно, успела зафиксировать. Компенсировать записанный заказ
		// нельзя — проверяем, есть ли он.
		checkCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.releaseTimeout)
		defer cancel()
		saved, getErr := s.store.Order(checkCtx, order.ID)
		switch {
		case getErr == nil:
			return saved, nil
		case !errors.Is(getErr, ErrNotFound):
			// Не знаем, записан ли заказ: резерв не трогаем (лучше
			// «зависший» резерв, чем заказ без товара) и громко журналируем.
			s.log.ErrorContext(ctx, "saga outcome unknown: order may exist, reservation kept",
				"order_id", order.ID, "err", err, "check_err", getErr)
			s.obs.Released(ReleaseReasonWriteFailed, fmt.Errorf("outcome unknown: %w", getErr))
			return Order{}, fmt.Errorf("create order %d: %w", order.ID, err)
		}
	}
	s.release(ctx, order.ID, ReleaseReasonWriteFailed)
	return Order{}, fmt.Errorf("create order %d: %w", order.ID, err)
}

// release возвращает резерв заказа — компенсация шага 1 или второй шаг
// отмены. Контекст отвязан от отмены запроса: клиент мог уйти (или истёк
// его дедлайн), а вернуть товар нужно в любом случае.
func (s *Service) release(ctx context.Context, orderID int64, reason string) error {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), s.releaseTimeout)
	defer cancel()
	err := s.warehouse.Release(ctx, orderID)
	s.obs.Released(reason, err)
	if err != nil {
		// Резерв остался висеть. В продакшне компенсацию стоит записать в
		// свою базу и повторять в фоне до успеха; здесь — журнал и метрика
		// orders_warehouse_releases_total{result="failed"} для алерта.
		s.log.ErrorContext(ctx, "release failed: reservation left behind",
			"order_id", orderID, "reason", reason, "err", err)
		return err
	}
	if reason != ReleaseReasonCancel {
		s.log.WarnContext(ctx, "saga compensated", "order_id", orderID, "reason", reason)
	}
	return nil
}

// CancelOrder отменяет заказ и возвращает товар на склад.
//
// Порядок: сначала отмена в своей базе (вместе с событием в outbox), потом
// Release. Наоборот было бы хуже: если бы Release прошёл, а запись — нет,
// заказ остался бы «зарезервированным» без товара на складе (и товар
// продали бы второй раз). При нашем порядке худший случай — заказ отменён,
// а резерв ещё висит; клиент получает ошибку и повторяет отмену.
//
// Повторная отмена уже отменённого заказа — не ошибка: метод возвращает
// заказ как есть и снова вызывает Release (идемпотентный). Так повтор
// дочищает резерв, если в прошлый раз склад не ответил.
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
		if err := tx.UpdateOrderStatus(ctx, id, o.Status, StatusCancelled, now); err != nil {
			return err
		}
		o.Status = StatusCancelled
		o.UpdatedAt = now
		order = o
		return s.addEvent(ctx, tx, o)
	})
	if err != nil {
		return Order{}, fmt.Errorf("cancel order %d: %w", id, err)
	}
	if err := s.release(ctx, id, ReleaseReasonCancel); err != nil {
		return Order{}, fmt.Errorf("cancel order %d: cancelled, but stock not released yet: %w", id, err)
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

// ListOrders возвращает страницу заказов покупателя (новые первыми) с
// id < beforeID (0 — с самого нового) и признак, что есть ещё страницы.
// Пагинация по ключу (keyset), а не OFFSET: страница не «съезжает» от
// новых заказов, и база не перебирает пропущенные строки.
func (s *Service) ListOrders(ctx context.Context, customerID, beforeID int64, limit int) ([]Order, bool, error) {
	if customerID <= 0 {
		return nil, false, &ValidationError{Field: "customer_id", Message: "must be positive"}
	}
	if limit < 1 || limit > 100 {
		return nil, false, &ValidationError{Field: "page_size", Message: "must be between 1 and 100"}
	}
	// На одну строку больше: так без COUNT(*) понятно, есть ли следующая страница.
	list, err := s.store.ListOrders(ctx, customerID, beforeID, limit+1)
	if err != nil {
		return nil, false, fmt.Errorf("list orders: %w", err)
	}
	if len(list) > limit {
		return list[:limit], true, nil
	}
	return list, false, nil
}

// commitError — сбой на COMMIT: исход транзакции неизвестен.
type commitError struct{ err error }

func (e *commitError) Error() string { return "commit: " + e.err.Error() }
func (e *commitError) Unwrap() error { return e.err }

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
	if err := tx.Commit(ctx); err != nil {
		return &commitError{err: err}
	}
	return nil
}

// validate проверяет запрос. Возвращает первую найденную проблему.
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
			return &ValidationError{Field: fmt.Sprintf("items[%d].product_id", i), Message: "duplicate product"}
		}
		seen[it.ProductID] = true
	}
	return nil
}
