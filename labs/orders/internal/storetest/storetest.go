// Package storetest — контрактный набор тестов для хранилищ «Заказов»
// (orders.Store и outbox.Store) и фейк «Склада» для тестов домена.
//
// Один набор запускается для каждой базы (см. store_test.go в пакетах
// internal/store/*): поведение, которое видит сервис, обязано совпадать
// у PostgreSQL и SQLite. Новый тест здесь сразу проверяет обе.
package storetest

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	"google.golang.org/protobuf/proto"

	eventsv1 "gotutor/labs/api/gen/events/v1"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/outbox"
)

// Store — хранилище целиком: домен и relay.
type Store interface {
	orders.Store
	outbox.Store
}

// NewStore возвращает пустое хранилище с применёнными миграциями
// (демо-каталог в нём есть — тесты заводят свои товары).
// Каждый вызов — отдельная база: тесты не видят данных друг друга.
type NewStore func(t *testing.T) Store

// Run запускает весь контрактный набор.
func Run(t *testing.T, newStore NewStore) {
	tests := []struct {
		name string
		fn   func(t *testing.T, s Store)
	}{
		{"ProductsByIDs", testProductsByIDs},
		{"NextOrderIDIsUnique", testNextOrderIDIsUnique},
		{"CreateOrder", testCreateOrder},
		{"OrderNotFound", testOrderNotFound},
		{"ListOrdersPages", testListOrdersPages},
		{"CancelOrder", testCancelOrder},
		{"CancelOrderTwiceIsIdempotent", testCancelOrderTwice},
		{"CancelOrderNotFound", testCancelOrderNotFound},
		{"RollbackDiscardsChanges", testRollbackDiscardsChanges},
		{"UpdateOrderStatusConflict", testUpdateOrderStatusConflict},
		{"ConcurrentCancel", testConcurrentCancel},
		{"PublishPending", testPublishPending},
		{"PublishFailureKeepsEvents", testPublishFailureKeepsEvents},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.fn(t, newStore(t))
		})
	}
}

// Now — фиксированные часы: время в тестах детерминировано. Наносекунды
// заданы нарочно — сервис обязан обрезать их до микросекунд.
var Now = time.Date(2026, 3, 14, 15, 9, 26, 535897932, time.UTC)

// Warehouse — фейк «Склада» в памяти: запоминает вызовы, ошибки задаются
// полями. Не моки с ожиданиями, а простая реализация интерфейса.
type Warehouse struct {
	mu         sync.Mutex
	reserved   map[int64][]orders.ItemRequest
	releases   map[int64]int
	ReserveErr error // вернуть из Reserve вместо резерва
	ReleaseErr error // вернуть из Release
}

// NewWarehouse создаёт фейк.
func NewWarehouse() *Warehouse {
	return &Warehouse{reserved: map[int64][]orders.ItemRequest{}, releases: map[int64]int{}}
}

// Reserve запоминает резерв.
func (w *Warehouse) Reserve(_ context.Context, orderID int64, items []orders.ItemRequest) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.ReserveErr != nil {
		return w.ReserveErr
	}
	w.reserved[orderID] = slices.Clone(items)
	return nil
}

// Release считает возвраты.
func (w *Warehouse) Release(_ context.Context, orderID int64) error {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.releases[orderID]++
	return w.ReleaseErr
}

// Reserved возвращает позиции резерва заказа.
func (w *Warehouse) Reserved(orderID int64) []orders.ItemRequest {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.reserved[orderID]
}

// Releases — сколько раз вызван Release для заказа.
func (w *Warehouse) Releases(orderID int64) int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.releases[orderID]
}

// Set меняет ошибки под блокировкой (тесты с горутинами).
func (w *Warehouse) Set(reserveErr, releaseErr error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.ReserveErr, w.ReleaseErr = reserveErr, releaseErr
}

func newService(s orders.Store, w orders.Warehouse) *orders.Service {
	return orders.NewService(s, w, orders.Options{Now: func() time.Time { return Now }})
}

func createProduct(t *testing.T, s orders.Store, name string, price int64) orders.Product {
	t.Helper()
	p, err := s.CreateProduct(t.Context(), orders.NewProduct{Name: name, PriceCents: price})
	if err != nil {
		t.Fatalf("CreateProduct(%q): %v", name, err)
	}
	return p
}

func createOrder(t *testing.T, svc *orders.Service, customerID int64, items ...orders.ItemRequest) orders.Order {
	t.Helper()
	o, err := svc.CreateOrder(t.Context(), orders.CreateOrderRequest{CustomerID: customerID, Items: items})
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	return o
}

// PendingEvents забирает неопубликованные события, не отмечая их
// (publish возвращает ошибку — отметки нет).
func PendingEvents(t *testing.T, s outbox.Store) []orders.Event {
	t.Helper()
	var got []orders.Event
	errPeek := errors.New("peek")
	_, err := s.PublishPending(t.Context(), 1000, func(_ context.Context, evs []orders.Event) error {
		got = slices.Clone(evs)
		return errPeek
	})
	if err != nil && !errors.Is(err, errPeek) {
		t.Fatalf("PublishPending: %v", err)
	}
	return got
}

func testProductsByIDs(t *testing.T, s Store) {
	a := createProduct(t, s, "Ручка «Паркер»", 129900)
	b := createProduct(t, s, "Блокнот", 30000)
	got, err := s.ProductsByIDs(t.Context(), []int64{b.ID, 999_999, a.ID})
	if err != nil {
		t.Fatalf("ProductsByIDs: %v", err)
	}
	want := []orders.Product{a, b}
	if a.ID > b.ID {
		want = []orders.Product{b, a}
	}
	if !slices.Equal(got, want) {
		t.Errorf("ProductsByIDs = %+v, want %+v (sorted, missing skipped)", got, want)
	}
}

func testNextOrderIDIsUnique(t *testing.T, s Store) {
	const n = 20
	ids := make(chan int64, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			id, err := s.NextOrderID(t.Context())
			if err != nil {
				t.Errorf("NextOrderID: %v", err)
				return
			}
			ids <- id
		})
	}
	wg.Wait()
	close(ids)
	seen := map[int64]bool{}
	for id := range ids {
		if id <= 0 || seen[id] {
			t.Errorf("id %d is not positive or repeated", id)
		}
		seen[id] = true
	}
}

func testCreateOrder(t *testing.T, s Store) {
	ctx := t.Context()
	pen := createProduct(t, s, "Ручка", 15000)
	pad := createProduct(t, s, "Блокнот", 30000)
	wh := NewWarehouse()
	svc := newService(s, wh)

	// Позиции нарочно не по порядку id: сервис их сортирует.
	created := createOrder(t, svc, 7,
		orders.ItemRequest{ProductID: pad.ID, Quantity: 2},
		orders.ItemRequest{ProductID: pen.ID, Quantity: 3},
	)
	wantTime := Now.Truncate(time.Microsecond)
	items := []orders.Item{
		{ProductID: pen.ID, Quantity: 3, PriceCents: 15000},
		{ProductID: pad.ID, Quantity: 2, PriceCents: 30000},
	}
	if pen.ID > pad.ID {
		items[0], items[1] = items[1], items[0]
	}
	want := orders.Order{
		ID: created.ID, CustomerID: 7, Status: orders.StatusReserved, Items: items,
		TotalCents: 3*15000 + 2*30000, CreatedAt: wantTime, UpdatedAt: wantTime,
	}
	assertOrder(t, "CreateOrder", created, want)
	got, err := s.Order(ctx, created.ID)
	if err != nil {
		t.Fatalf("Order: %v", err)
	}
	assertOrder(t, "Order", got, want)

	if r := wh.Reserved(created.ID); len(r) != 2 {
		t.Errorf("warehouse reserve for order %d = %v", created.ID, r)
	}

	events := PendingEvents(t, s)
	if len(events) != 1 {
		t.Fatalf("pending events = %d, want 1", len(events))
	}
	ev := events[0]
	if ev.Type != orders.EventOrderCreated || ev.OrderID != created.ID || ev.EventID == "" || !ev.CreatedAt.Equal(wantTime) {
		t.Errorf("event = %+v", ev)
	}
	var payload eventsv1.OrderEvent
	if err := proto.Unmarshal(ev.Payload, &payload); err != nil {
		t.Fatalf("payload: %v", err)
	}
	oc := payload.GetOrderCreated()
	if payload.GetEventId() != ev.EventID || payload.GetOrderId() != created.ID || oc.GetCustomerId() != 7 ||
		oc.GetTotalCents() != want.TotalCents || len(oc.GetItems()) != 2 {
		t.Errorf("payload = %v", &payload)
	}
}

func testOrderNotFound(t *testing.T, s Store) {
	if _, err := s.Order(t.Context(), 999_999); !errors.Is(err, orders.ErrNotFound) {
		t.Fatalf("Order(missing) error = %v, want ErrNotFound", err)
	}
}

func testListOrdersPages(t *testing.T, s Store) {
	p := createProduct(t, s, "Ручка", 100)
	svc := newService(s, NewWarehouse())
	var mine []int64
	for i := range 5 {
		mine = append(mine, createOrder(t, svc, 7, orders.ItemRequest{ProductID: p.ID, Quantity: i + 1}).ID)
		createOrder(t, svc, 8, orders.ItemRequest{ProductID: p.ID, Quantity: 1})
	}
	slices.Reverse(mine) // новые первыми

	var got []int64
	var before int64
	for page := 0; ; page++ {
		list, more, err := svc.ListOrders(t.Context(), 7, before, 2)
		if err != nil {
			t.Fatalf("ListOrders: %v", err)
		}
		for _, o := range list {
			if o.CustomerID != 7 || len(o.Items) != 1 {
				t.Errorf("order %+v: wrong customer or items", o)
			}
			got = append(got, o.ID)
		}
		if !more {
			break
		}
		if page > 5 {
			t.Fatal("pagination does not terminate")
		}
		before = list[len(list)-1].ID
	}
	if !slices.Equal(got, mine) {
		t.Errorf("pages = %v, want %v", got, mine)
	}
	if list, more, err := svc.ListOrders(t.Context(), 9, 0, 10); err != nil || len(list) != 0 || more {
		t.Errorf("unknown customer: %v, %v, %v", list, more, err)
	}
}

func testCancelOrder(t *testing.T, s Store) {
	p := createProduct(t, s, "Ручка", 100)
	wh := NewWarehouse()
	svc := newService(s, wh)
	o := createOrder(t, svc, 7, orders.ItemRequest{ProductID: p.ID, Quantity: 1})

	cancelled, err := svc.CancelOrder(t.Context(), o.ID)
	if err != nil {
		t.Fatalf("CancelOrder: %v", err)
	}
	if cancelled.Status != orders.StatusCancelled {
		t.Errorf("status = %q", cancelled.Status)
	}
	got, _ := s.Order(t.Context(), o.ID)
	if got.Status != orders.StatusCancelled {
		t.Errorf("stored status = %q", got.Status)
	}
	if wh.Releases(o.ID) != 1 {
		t.Errorf("releases = %d, want 1", wh.Releases(o.ID))
	}
	events := PendingEvents(t, s)
	if len(events) != 2 || events[1].Type != orders.EventOrderCancelled {
		t.Fatalf("events = %+v, want created then cancelled", events)
	}
}

func testCancelOrderTwice(t *testing.T, s Store) {
	p := createProduct(t, s, "Ручка", 100)
	wh := NewWarehouse()
	svc := newService(s, wh)
	o := createOrder(t, svc, 7, orders.ItemRequest{ProductID: p.ID, Quantity: 1})
	first, err := svc.CancelOrder(t.Context(), o.ID)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CancelOrder(t.Context(), o.ID)
	if err != nil {
		t.Fatalf("second CancelOrder: %v", err)
	}
	assertOrder(t, "second cancel", second, first)
	// Событие об отмене — одно, а Release повторяется (идемпотентен на складе).
	if n := len(PendingEvents(t, s)); n != 2 {
		t.Errorf("events = %d, want 2", n)
	}
	if wh.Releases(o.ID) != 2 {
		t.Errorf("releases = %d, want 2", wh.Releases(o.ID))
	}
}

func testCancelOrderNotFound(t *testing.T, s Store) {
	wh := NewWarehouse()
	_, err := newService(s, wh).CancelOrder(t.Context(), 999_999)
	if !errors.Is(err, orders.ErrNotFound) {
		t.Fatalf("error = %v, want ErrNotFound", err)
	}
	if wh.Releases(999_999) != 0 {
		t.Error("Release called for a missing order")
	}
}

func testRollbackDiscardsChanges(t *testing.T, s Store) {
	ctx := t.Context()
	p := createProduct(t, s, "Ручка", 100)
	id, err := s.NextOrderID(ctx)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	o := orders.Order{ID: id, CustomerID: 1, Status: orders.StatusReserved, TotalCents: 100,
		Items: []orders.Item{{ProductID: p.ID, Quantity: 1, PriceCents: 100}}, CreatedAt: Now, UpdatedAt: Now}
	if err := tx.CreateOrder(ctx, o); err != nil {
		t.Fatal(err)
	}
	if err := tx.AddEvent(ctx, orders.Event{EventID: "e1", Type: orders.EventOrderCreated, OrderID: id, Payload: []byte{1}, CreatedAt: Now}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Order(ctx, id); !errors.Is(err, orders.ErrNotFound) {
		t.Errorf("order after rollback: %v, want ErrNotFound", err)
	}
	if n := len(PendingEvents(t, s)); n != 0 {
		t.Errorf("events after rollback = %d", n)
	}
}

func testUpdateOrderStatusConflict(t *testing.T, s Store) {
	ctx := t.Context()
	p := createProduct(t, s, "Ручка", 100)
	o := createOrder(t, newService(s, NewWarehouse()), 7, orders.ItemRequest{ProductID: p.ID, Quantity: 1})
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	err = tx.UpdateOrderStatus(ctx, o.ID, orders.StatusCancelled, orders.StatusReserved, Now)
	if !errors.Is(err, orders.ErrConflict) {
		t.Errorf("error = %v, want ErrConflict", err)
	}
}

func testConcurrentCancel(t *testing.T, s Store) {
	p := createProduct(t, s, "Ручка", 100)
	svc := newService(s, NewWarehouse())
	o := createOrder(t, svc, 7, orders.ItemRequest{ProductID: p.ID, Quantity: 1})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			if _, err := svc.CancelOrder(t.Context(), o.ID); err != nil {
				t.Errorf("CancelOrder: %v", err)
			}
		})
	}
	wg.Wait()
	cancelled := 0
	for _, ev := range PendingEvents(t, s) {
		if ev.Type == orders.EventOrderCancelled {
			cancelled++
		}
	}
	if cancelled != 1 {
		t.Errorf("cancelled events = %d, want exactly 1", cancelled)
	}
}

func addEvents(t *testing.T, s Store, n int) []string {
	t.Helper()
	ctx := t.Context()
	tx, err := s.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	var ids []string
	for i := range n {
		id := "event-" + string(rune('a'+i))
		ids = append(ids, id)
		err := tx.AddEvent(ctx, orders.Event{
			EventID: id, Type: orders.EventOrderCreated, OrderID: int64(i + 1),
			Payload:      []byte{byte(i)},
			TraceContext: map[string]string{"traceparent": "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01"},
			CreatedAt:    Now.Add(time.Duration(i) * time.Second).Truncate(time.Microsecond),
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return ids
}

func testPublishPending(t *testing.T, s Store) {
	ctx := t.Context()
	want := addEvents(t, s, 3)

	pending, oldest, err := s.PendingStats(ctx)
	if err != nil || pending != 3 || !oldest.Equal(Now.Truncate(time.Microsecond)) {
		t.Fatalf("PendingStats = %d, %v, %v; want 3, %v", pending, oldest, err, Now)
	}

	// Пачками по 2: порядок — порядок записи.
	var got []string
	for range 2 {
		_, err := s.PublishPending(ctx, 2, func(_ context.Context, evs []orders.Event) error {
			for _, ev := range evs {
				got = append(got, ev.EventID)
				if !maps.Equal(ev.TraceContext, map[string]string{"traceparent": "00-0102030405060708090a0b0c0d0e0f10-0102030405060708-01"}) {
					t.Errorf("trace context = %v", ev.TraceContext)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("PublishPending: %v", err)
		}
	}
	if !slices.Equal(got, want) {
		t.Errorf("published %v, want %v", got, want)
	}
	n, err := s.PublishPending(ctx, 10, func(context.Context, []orders.Event) error {
		t.Error("publish called with nothing pending")
		return nil
	})
	if err != nil || n != 0 {
		t.Errorf("PublishPending on empty outbox = %d, %v", n, err)
	}
	if pending, oldest, err := s.PendingStats(ctx); err != nil || pending != 0 || !oldest.IsZero() {
		t.Errorf("PendingStats after publish = %d, %v, %v", pending, oldest, err)
	}
}

func testPublishFailureKeepsEvents(t *testing.T, s Store) {
	ctx := t.Context()
	addEvents(t, s, 2)
	errBroker := errors.New("broker down")
	n, err := s.PublishPending(ctx, 10, func(context.Context, []orders.Event) error { return errBroker })
	if !errors.Is(err, errBroker) || n != 0 {
		t.Fatalf("PublishPending = %d, %v; want 0, broker error", n, err)
	}
	// Ничего не отмечено: следующий такт отправит те же события.
	n, err = s.PublishPending(ctx, 10, func(context.Context, []orders.Event) error { return nil })
	if err != nil || n != 2 {
		t.Fatalf("retry PublishPending = %d, %v; want 2", n, err)
	}
}

func assertOrder(t *testing.T, what string, got, want orders.Order) {
	t.Helper()
	if got.ID != want.ID || got.CustomerID != want.CustomerID || got.Status != want.Status ||
		got.TotalCents != want.TotalCents || !got.CreatedAt.Equal(want.CreatedAt) ||
		!slices.Equal(got.Items, want.Items) {
		t.Errorf("%s = %+v, want %+v", what, got, want)
	}
}

// CleanupContext — контекст для удаления тестовой базы в t.Cleanup:
// t.Context() к этому моменту уже отменён.
func CleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}
