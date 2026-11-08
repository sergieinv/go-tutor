// Package storetest — контрактный набор тестов для реализаций orders.Store.
//
// Один набор запускается для каждой базы (см. store_test.go в пакетах
// internal/store/*): поведение, которое видит сервис, обязано совпадать
// у PostgreSQL, MySQL и SQLite. Новый тест здесь сразу проверяет все три.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"gotutor/labs/orders/internal/orders"
)

// NewStore возвращает пустое хранилище с применёнными миграциями.
// Каждый вызов — отдельная база: тесты не видят данных друг друга.
// Закрытие и удаление базы — забота функции (через t.Cleanup).
type NewStore func(t *testing.T) orders.Store

// Run запускает весь контрактный набор.
func Run(t *testing.T, newStore NewStore) {
	tests := []struct {
		name string
		fn   func(t *testing.T, s orders.Store)
	}{
		{"ProductRoundTrip", testProductRoundTrip},
		{"ProductNotFound", testProductNotFound},
		{"CreateOrder", testCreateOrder},
		{"CreateOrderOutOfStockIsAtomic", testCreateOrderOutOfStockIsAtomic},
		{"CreateOrderUnknownProduct", testCreateOrderUnknownProduct},
		{"OrderNotFound", testOrderNotFound},
		{"CancelOrder", testCancelOrder},
		{"CancelOrderTwiceIsIdempotent", testCancelOrderTwice},
		{"CancelOrderNotFound", testCancelOrderNotFound},
		{"RollbackDiscardsChanges", testRollbackDiscardsChanges},
		{"ReserveStockInsufficient", testReserveStockInsufficient},
		{"UpdateOrderStatusConflict", testUpdateOrderStatusConflict},
		{"ConcurrentReservation", testConcurrentReservation},
		{"ConcurrentCancel", testConcurrentCancel},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, newStore(t))
		})
	}
}

// now — фиксированные часы: время в тестах детерминировано. Наносекунды
// заданы нарочно — сервис обязан обрезать их до микросекунд.
var now = time.Date(2026, 3, 14, 15, 9, 26, 535897932, time.UTC)

func newService(s orders.Store) *orders.Service {
	return orders.NewService(s, func() time.Time { return now })
}

func createProduct(t *testing.T, s orders.Store, name string, price int64, available int) orders.Product {
	t.Helper()
	p, err := s.CreateProduct(t.Context(), orders.NewProduct{Name: name, PriceCents: price, Available: available})
	if err != nil {
		t.Fatalf("CreateProduct(%q): %v", name, err)
	}
	return p
}

func available(t *testing.T, s orders.Store, productID int64) int {
	t.Helper()
	p, err := s.Product(t.Context(), productID)
	if err != nil {
		t.Fatalf("Product(%d): %v", productID, err)
	}
	return p.Available
}

func pendingEvents(t *testing.T, s orders.Store) []orders.Event {
	t.Helper()
	events, err := s.PendingEvents(t.Context(), 1000)
	if err != nil {
		t.Fatalf("PendingEvents: %v", err)
	}
	return events
}

func testProductRoundTrip(t *testing.T, s orders.Store) {
	p := createProduct(t, s, "Ручка «Паркер»", 129900, 42)
	if p.ID <= 0 {
		t.Fatalf("CreateProduct: ID = %d, want positive", p.ID)
	}
	got, err := s.Product(t.Context(), p.ID)
	if err != nil {
		t.Fatalf("Product: %v", err)
	}
	if got != p {
		t.Errorf("Product = %+v, want %+v", got, p)
	}
}

func testProductNotFound(t *testing.T, s orders.Store) {
	_, err := s.Product(t.Context(), 999_999)
	if !errors.Is(err, orders.ErrNotFound) {
		t.Fatalf("Product(missing) error = %v, want ErrNotFound", err)
	}
}

func testCreateOrder(t *testing.T, s orders.Store) {
	ctx := t.Context()
	pen := createProduct(t, s, "Ручка", 15000, 10)
	pad := createProduct(t, s, "Блокнот", 30000, 5)
	svc := newService(s)

	// Позиции нарочно не по порядку id: сервис их сортирует.
	created, err := svc.CreateOrder(ctx, orders.CreateOrderRequest{
		CustomerID: 7,
		Items: []orders.ItemRequest{
			{ProductID: pad.ID, Quantity: 2},
			{ProductID: pen.ID, Quantity: 3},
		},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	wantTime := now.Truncate(time.Microsecond)
	want := orders.Order{
		ID:         created.ID,
		CustomerID: 7,
		Status:     orders.StatusReserved,
		Items: []orders.Item{
			{ProductID: pen.ID, Quantity: 3, PriceCents: 15000},
			{ProductID: pad.ID, Quantity: 2, PriceCents: 30000},
		},
		TotalCents: 3*15000 + 2*30000,
		CreatedAt:  wantTime,
		UpdatedAt:  wantTime,
	}
	if created.ID <= 0 {
		t.Fatalf("CreateOrder: ID = %d, want positive", created.ID)
	}
	assertOrder(t, "CreateOrder", created, want)

	got, err := s.Order(ctx, created.ID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	assertOrder(t, "Order", got, want)

	if n := available(t, s, pen.ID); n != 7 {
		t.Errorf("pen available = %d, want 7", n)
	}
	if n := available(t, s, pad.ID); n != 3 {
		t.Errorf("pad available = %d, want 3", n)
	}

	events := pendingEvents(t, s)
	if len(events) != 1 {
		t.Fatalf("outbox has %d events, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != orders.EventOrderCreated || ev.OrderID != created.ID || !ev.CreatedAt.Equal(wantTime) {
		t.Errorf("event = {Type:%q OrderID:%d CreatedAt:%v}, want {%q %d %v}",
			ev.Type, ev.OrderID, ev.CreatedAt, orders.EventOrderCreated, created.ID, wantTime)
	}
	var payload struct {
		OrderID    int64 `json:"order_id"`
		TotalCents int64 `json:"total_cents"`
		Items      []struct {
			ProductID int64 `json:"product_id"`
			Quantity  int   `json:"quantity"`
		} `json:"items"`
	}
	if err := json.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("event payload %s: %v", ev.Payload, err)
	}
	if payload.OrderID != created.ID || payload.TotalCents != want.TotalCents || len(payload.Items) != 2 {
		t.Errorf("event payload = %s, want order %d, total %d, 2 items", ev.Payload, created.ID, want.TotalCents)
	}
}

func testCreateOrderOutOfStockIsAtomic(t *testing.T, s orders.Store) {
	ctx := t.Context()
	// Первая позиция (меньший id) резервируется успешно, вторая — нет.
	// Транзакция обязана откатить и первую.
	plenty := createProduct(t, s, "Много", 100, 10)
	scarce := createProduct(t, s, "Мало", 100, 1)

	_, err := newService(s).CreateOrder(ctx, orders.CreateOrderRequest{
		CustomerID: 1,
		Items: []orders.ItemRequest{
			{ProductID: plenty.ID, Quantity: 5},
			{ProductID: scarce.ID, Quantity: 2},
		},
	})
	if !errors.Is(err, orders.ErrOutOfStock) {
		t.Fatalf("CreateOrder error = %v, want ErrOutOfStock", err)
	}
	if n := available(t, s, plenty.ID); n != 10 {
		t.Errorf("first item available = %d, want 10 (reservation rolled back)", n)
	}
	if n := available(t, s, scarce.ID); n != 1 {
		t.Errorf("second item available = %d, want 1", n)
	}
	if events := pendingEvents(t, s); len(events) != 0 {
		t.Errorf("outbox has %d events after failed order, want 0", len(events))
	}
}

func testCreateOrderUnknownProduct(t *testing.T, s orders.Store) {
	p := createProduct(t, s, "Есть", 100, 10)
	_, err := newService(s).CreateOrder(t.Context(), orders.CreateOrderRequest{
		CustomerID: 1,
		Items: []orders.ItemRequest{
			{ProductID: p.ID, Quantity: 1},
			{ProductID: 999_999, Quantity: 1},
		},
	})
	var ve *orders.ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("CreateOrder error = %v, want ValidationError", err)
	}
	if ve.Field != "items[1].product_id" {
		t.Errorf("ValidationError.Field = %q, want items[1].product_id", ve.Field)
	}
	if n := available(t, s, p.ID); n != 10 {
		t.Errorf("available = %d, want 10", n)
	}
}

func testOrderNotFound(t *testing.T, s orders.Store) {
	_, err := s.Order(t.Context(), 999_999)
	if !errors.Is(err, orders.ErrNotFound) {
		t.Fatalf("Order(missing) error = %v, want ErrNotFound", err)
	}
}

func testCancelOrder(t *testing.T, s orders.Store) {
	ctx := t.Context()
	p := createProduct(t, s, "Товар", 500, 10)
	svc := newService(s)
	created, err := svc.CreateOrder(ctx, orders.CreateOrderRequest{
		CustomerID: 1,
		Items:      []orders.ItemRequest{{ProductID: p.ID, Quantity: 4}},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	cancelled, err := svc.CancelOrder(ctx, created.ID)
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if cancelled.Status != orders.StatusCancelled {
		t.Errorf("CancelOrder status = %q, want %q", cancelled.Status, orders.StatusCancelled)
	}
	got, err := s.Order(ctx, created.ID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	if got.Status != orders.StatusCancelled {
		t.Errorf("stored status = %q, want %q", got.Status, orders.StatusCancelled)
	}
	if n := available(t, s, p.ID); n != 10 {
		t.Errorf("available after cancel = %d, want 10", n)
	}
	events := pendingEvents(t, s)
	if len(events) != 2 || events[0].Type != orders.EventOrderCreated || events[1].Type != orders.EventOrderCancelled {
		t.Errorf("outbox = %v, want [order.created order.cancelled]", eventTypes(events))
	}
}

func testCancelOrderTwice(t *testing.T, s orders.Store) {
	ctx := t.Context()
	p := createProduct(t, s, "Товар", 500, 10)
	svc := newService(s)
	created, err := svc.CreateOrder(ctx, orders.CreateOrderRequest{
		CustomerID: 1,
		Items:      []orders.ItemRequest{{ProductID: p.ID, Quantity: 4}},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	for i := range 2 {
		o, err := svc.CancelOrder(ctx, created.ID)
		if err != nil {
			t.Fatalf("CancelOrder #%d: %v", i+1, err)
		}
		if o.Status != orders.StatusCancelled {
			t.Fatalf("CancelOrder #%d status = %q", i+1, o.Status)
		}
	}
	// Остаток вернулся ровно один раз.
	if n := available(t, s, p.ID); n != 10 {
		t.Errorf("available after double cancel = %d, want 10", n)
	}
	if events := pendingEvents(t, s); len(events) != 2 {
		t.Errorf("outbox = %v, want exactly one order.cancelled", eventTypes(events))
	}
}

func testCancelOrderNotFound(t *testing.T, s orders.Store) {
	_, err := newService(s).CancelOrder(t.Context(), 999_999)
	if !errors.Is(err, orders.ErrNotFound) {
		t.Fatalf("CancelOrder(missing) error = %v, want ErrNotFound", err)
	}
}

func testRollbackDiscardsChanges(t *testing.T, s orders.Store) {
	ctx := t.Context()
	p := createProduct(t, s, "Товар", 500, 10)

	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if err := tx.ReserveStock(ctx, p.ID, 3); err != nil {
		t.Fatalf("ReserveStock: %v", err)
	}
	if err := tx.AddEvent(ctx, orders.Event{Type: "test", OrderID: 1, Payload: []byte(`{}`), CreatedAt: now}); err != nil {
		t.Fatalf("AddEvent: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	if n := available(t, s, p.ID); n != 10 {
		t.Errorf("available after rollback = %d, want 10", n)
	}
	if events := pendingEvents(t, s); len(events) != 0 {
		t.Errorf("outbox after rollback = %v, want empty", eventTypes(events))
	}
}

func testReserveStockInsufficient(t *testing.T, s orders.Store) {
	ctx := t.Context()
	p := createProduct(t, s, "Товар", 500, 2)

	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	if err := tx.ReserveStock(ctx, p.ID, 3); !errors.Is(err, orders.ErrOutOfStock) {
		t.Fatalf("ReserveStock(3 of 2) error = %v, want ErrOutOfStock", err)
	}
	// Ровно весь остаток — можно.
	if err := tx.ReserveStock(ctx, p.ID, 2); err != nil {
		t.Fatalf("ReserveStock(2 of 2): %v", err)
	}
	if err := tx.ReserveStock(ctx, 999_999, 1); !errors.Is(err, orders.ErrOutOfStock) {
		t.Fatalf("ReserveStock(missing product) error = %v, want ErrOutOfStock", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("Commit: %v", err)
	}
	if n := available(t, s, p.ID); n != 0 {
		t.Errorf("available = %d, want 0", n)
	}
}

func testUpdateOrderStatusConflict(t *testing.T, s orders.Store) {
	ctx := t.Context()
	p := createProduct(t, s, "Товар", 500, 10)
	created, err := newService(s).CreateOrder(ctx, orders.CreateOrderRequest{
		CustomerID: 1,
		Items:      []orders.ItemRequest{{ProductID: p.ID, Quantity: 1}},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	defer tx.Rollback(ctx)
	// Заказ в статусе reserved, а мы утверждаем, что он new.
	err = tx.UpdateOrderStatus(ctx, created.ID, orders.StatusNew, orders.StatusCancelled, now)
	if !errors.Is(err, orders.ErrConflict) {
		t.Fatalf("UpdateOrderStatus(wrong from) error = %v, want ErrConflict", err)
	}
}

// testConcurrentReservation: много покупателей одновременно заказывают
// товар, которого на всех не хватит. Проверяем инварианты, а не время:
// продано ровно столько, сколько было, каждый успешный заказ учтён, каждый
// неуспешный получил ErrOutOfStock. Позиции в половине заказов перечислены
// в обратном порядке — без сортировки в сервисе PostgreSQL и MySQL
// ловили бы взаимоблокировки.
func testConcurrentReservation(t *testing.T, s orders.Store) {
	const (
		buyers = 24
		stock  = 7
	)
	ctx := t.Context()
	scarce := createProduct(t, s, "Дефицит", 1000, stock)
	plenty := createProduct(t, s, "В достатке", 10, 1000)
	svc := newService(s)

	type result struct {
		order orders.Order
		err   error
	}
	results := make([]result, buyers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range buyers {
		items := []orders.ItemRequest{{ProductID: scarce.ID, Quantity: 1}, {ProductID: plenty.ID, Quantity: 1}}
		if i%2 == 1 {
			items[0], items[1] = items[1], items[0]
		}
		wg.Go(func() {
			<-start // все горутины стартуют разом — максимум конкуренции
			o, err := svc.CreateOrder(ctx, orders.CreateOrderRequest{CustomerID: int64(i + 1), Items: items})
			results[i] = result{o, err}
		})
	}
	close(start)
	wg.Wait()

	var ok []orders.Order
	for i, r := range results {
		switch {
		case r.err == nil:
			ok = append(ok, r.order)
		case errors.Is(r.err, orders.ErrOutOfStock):
		default:
			t.Errorf("buyer %d: unexpected error: %v", i, r.err)
		}
	}
	if len(ok) != stock {
		t.Errorf("successful orders = %d, want %d (stock)", len(ok), stock)
	}
	if n := available(t, s, scarce.ID); n != 0 {
		t.Errorf("scarce available = %d, want 0", n)
	}
	if n := available(t, s, plenty.ID); n != 1000-len(ok) {
		t.Errorf("plenty available = %d, want %d", n, 1000-len(ok))
	}
	// Каждый успешный заказ действительно в базе, и в outbox по событию на заказ.
	reserved := 0
	for _, o := range ok {
		got, err := s.Order(ctx, o.ID)
		if err != nil {
			t.Fatalf("Order(%d): %v", o.ID, err)
		}
		for _, it := range got.Items {
			if it.ProductID == scarce.ID {
				reserved += it.Quantity
			}
		}
	}
	if reserved != stock {
		t.Errorf("reserved in stored orders = %d, want %d", reserved, stock)
	}
	if events := pendingEvents(t, s); len(events) != len(ok) {
		t.Errorf("outbox has %d events, want %d", len(events), len(ok))
	}
}

// testConcurrentCancel: один заказ отменяют параллельно. Остаток должен
// вернуться ровно один раз, событие отмены — одно, а все вызовы — успешны
// (повторная отмена идемпотентна, блокировка строки упорядочивает их).
func testConcurrentCancel(t *testing.T, s orders.Store) {
	const cancels = 12
	ctx := t.Context()
	p := createProduct(t, s, "Товар", 500, 10)
	svc := newService(s)
	created, err := svc.CreateOrder(ctx, orders.CreateOrderRequest{
		CustomerID: 1,
		Items:      []orders.ItemRequest{{ProductID: p.ID, Quantity: 6}},
	})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}

	errs := make([]error, cancels)
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := range cancels {
		wg.Go(func() {
			<-start
			_, errs[i] = svc.CancelOrder(ctx, created.ID)
		})
	}
	close(start)
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("cancel %d: %v", i, err)
		}
	}
	if n := available(t, s, p.ID); n != 10 {
		t.Errorf("available after concurrent cancels = %d, want 10", n)
	}
	events := pendingEvents(t, s)
	cancelled := 0
	for _, ev := range events {
		if ev.Type == orders.EventOrderCancelled {
			cancelled++
		}
	}
	if cancelled != 1 {
		t.Errorf("order.cancelled events = %d, want 1", cancelled)
	}
}

func assertOrder(t *testing.T, what string, got, want orders.Order) {
	t.Helper()
	// time.Time сравниваем через Equal: у значений из базы может отличаться
	// внутреннее представление зоны.
	if got.ID != want.ID || got.CustomerID != want.CustomerID || got.Status != want.Status ||
		got.TotalCents != want.TotalCents || !got.CreatedAt.Equal(want.CreatedAt) || !got.UpdatedAt.Equal(want.UpdatedAt) ||
		!slices.Equal(got.Items, want.Items) {
		t.Errorf("%s =\n\t%+v\nwant\n\t%+v", what, got, want)
	}
}

func eventTypes(events []orders.Event) []string {
	types := make([]string, len(events))
	for i, ev := range events {
		types[i] = ev.Type
	}
	return types
}

// CleanupContext — контекст для удаления тестовой базы в t.Cleanup:
// t.Context() к этому моменту уже отменён.
func CleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
