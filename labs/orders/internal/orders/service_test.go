package orders_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	eventsv1 "gotutor/labs/api/gen/events/v1"
	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/orders/internal/store/sqlite"
	"gotutor/labs/orders/internal/storetest"
)

// Тесты саги: настоящее хранилище (SQLite во временном файле) и фейк
// склада. Сбои базы — обёртка над хранилищем, а не мок.

func newStore(t *testing.T) *sqlite.Store {
	t.Helper()
	s, err := sqlite.Open(t.Context(), filepath.Join(t.TempDir(), "orders.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(s.Close)
	if err := s.Migrate(t.Context()); err != nil {
		t.Fatal(err)
	}
	return s
}

// observer запоминает сообщения о возвратах резерва.
type observer struct {
	mu       sync.Mutex
	released []string // "reason:ok" / "reason:failed"
}

func (o *observer) Released(reason string, err error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	res := "ok"
	if err != nil {
		res = "failed"
	}
	o.released = append(o.released, reason+":"+res)
}

func (o *observer) list() []string {
	o.mu.Lock()
	defer o.mu.Unlock()
	return append([]string(nil), o.released...)
}

type fixture struct {
	store *sqlite.Store
	wh    *storetest.Warehouse
	obs   *observer
	svc   *orders.Service
	pen   orders.Product
}

func newFixture(t *testing.T, wrap func(orders.Store) orders.Store) *fixture {
	t.Helper()
	f := &fixture{store: newStore(t), wh: storetest.NewWarehouse(), obs: &observer{}}
	var st orders.Store = f.store
	if wrap != nil {
		st = wrap(f.store)
	}
	f.svc = orders.NewService(st, f.wh, orders.Options{
		Now:      func() time.Time { return storetest.Now },
		Observer: f.obs,
	})
	var err error
	if f.pen, err = f.store.CreateProduct(t.Context(), orders.NewProduct{Name: "Ручка", PriceCents: 15000}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f *fixture) create(t *testing.T) (orders.Order, error) {
	return f.svc.CreateOrder(t.Context(), orders.CreateOrderRequest{
		CustomerID: 7,
		Items:      []orders.ItemRequest{{ProductID: f.pen.ID, Quantity: 2}},
	})
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCreateOrderHappyPath(t *testing.T) {
	f := newFixture(t, nil)
	o, err := f.create(t)
	if err != nil {
		t.Fatalf("CreateOrder: %v", err)
	}
	if o.Status != orders.StatusReserved || o.TotalCents != 30000 {
		t.Errorf("order = %+v", o)
	}
	if f.wh.Reserved(o.ID) == nil || f.wh.Releases(o.ID) != 0 {
		t.Errorf("reserve/release calls: reserved=%v releases=%d", f.wh.Reserved(o.ID), f.wh.Releases(o.ID))
	}
	if got := f.obs.list(); len(got) != 0 {
		t.Errorf("observer = %v, want nothing", got)
	}
}

func TestCreateOrderOutOfStockDoesNotCompensate(t *testing.T) {
	f := newFixture(t, nil)
	f.wh.Set(&orders.OutOfStockError{ProductIDs: []int64{f.pen.ID}}, nil)
	_, err := f.create(t)
	var oos *orders.OutOfStockError
	if !errors.As(err, &oos) || !errors.Is(err, orders.ErrOutOfStock) {
		t.Fatalf("error = %v, want OutOfStockError", err)
	}
	// Точный отказ склада: резерва нет, отменять нечего.
	if got := f.obs.list(); len(got) != 0 {
		t.Errorf("observer = %v, want no compensation", got)
	}
	if n := len(storetest.PendingEvents(t, f.store)); n != 0 {
		t.Errorf("events = %d, want 0", n)
	}
}

func TestCreateOrderReserveUnknownOutcomeCompensates(t *testing.T) {
	f := newFixture(t, nil)
	f.wh.Set(orders.ErrWarehouseUnavailable, nil)
	_, err := f.create(t)
	if !errors.Is(err, orders.ErrWarehouseUnavailable) {
		t.Fatalf("error = %v, want ErrWarehouseUnavailable", err)
	}
	if got := f.obs.list(); !equal(got, []string{"reserve_failed:ok"}) {
		t.Errorf("observer = %v, want [reserve_failed:ok]", got)
	}
}

// failingStore ломает запись заказа: до коммита или на коммите.
type failingStore struct {
	orders.Store
	failInsert bool
	// commit: "" — обычный, "lost" — откатить и вернуть ошибку,
	// "committed" — зафиксировать и вернуть ошибку (обрыв после COMMIT).
	commit string
}

func (s *failingStore) Begin(ctx context.Context) (orders.Tx, error) {
	tx, err := s.Store.Begin(ctx)
	if err != nil {
		return nil, err
	}
	return &failingTx{Tx: tx, s: s}, nil
}

type failingTx struct {
	orders.Tx
	s *failingStore
}

var errDB = errors.New("connection reset by peer")

func (t *failingTx) CreateOrder(ctx context.Context, o orders.Order) error {
	if t.s.failInsert {
		return errDB
	}
	return t.Tx.CreateOrder(ctx, o)
}

func (t *failingTx) Commit(ctx context.Context) error {
	switch t.s.commit {
	case "lost":
		_ = t.Tx.Rollback(ctx)
		return errDB
	case "committed":
		if err := t.Tx.Commit(ctx); err != nil {
			return err
		}
		return errDB
	}
	return t.Tx.Commit(ctx)
}

func TestCreateOrderWriteFailureCompensates(t *testing.T) {
	for _, tt := range []struct {
		name string
		fs   failingStore
	}{
		{"insert fails", failingStore{failInsert: true}},
		{"commit lost", failingStore{commit: "lost"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			fs := tt.fs
			f := newFixture(t, func(s orders.Store) orders.Store { fs.Store = s; return &fs })
			_, err := f.create(t)
			if !errors.Is(err, errDB) {
				t.Fatalf("error = %v, want DB error", err)
			}
			if got := f.obs.list(); !equal(got, []string{"write_failed:ok"}) {
				t.Errorf("observer = %v, want [write_failed:ok]", got)
			}
			if n := len(storetest.PendingEvents(t, f.store)); n != 0 {
				t.Errorf("events = %d, want 0", n)
			}
		})
	}
}

func TestCreateOrderCommitErrorButCommittedKeepsReservation(t *testing.T) {
	fs := failingStore{commit: "committed"}
	f := newFixture(t, func(s orders.Store) orders.Store { fs.Store = s; return &fs })
	o, err := f.create(t)
	if err != nil {
		t.Fatalf("CreateOrder: %v (order was committed, must be returned)", err)
	}
	if f.wh.Releases(o.ID) != 0 {
		t.Error("reservation released for a committed order")
	}
}

func TestCompensationFailureIsReported(t *testing.T) {
	fs := failingStore{failInsert: true}
	f := newFixture(t, func(s orders.Store) orders.Store { fs.Store = s; return &fs })
	f.wh.Set(nil, orders.ErrWarehouseUnavailable)
	if _, err := f.create(t); !errors.Is(err, errDB) {
		t.Fatalf("error = %v", err)
	}
	if got := f.obs.list(); !equal(got, []string{"write_failed:failed"}) {
		t.Errorf("observer = %v, want [write_failed:failed]", got)
	}
}

// cancellingWarehouse — клиент «уходит» во время резерва: Reserve отменяет
// контекст запроса. Release проверяет, что получил живой контекст.
type cancellingWarehouse struct {
	cancel       context.CancelFunc
	releaseCtxOK bool
}

func (w *cancellingWarehouse) Reserve(ctx context.Context, _ int64, _ []orders.ItemRequest) error {
	w.cancel()
	return ctx.Err()
}

func (w *cancellingWarehouse) Release(ctx context.Context, _ int64) error {
	w.releaseCtxOK = ctx.Err() == nil
	return ctx.Err()
}

func TestCompensationRunsAfterClientCancel(t *testing.T) {
	store := newStore(t)
	pen, err := store.CreateProduct(t.Context(), orders.NewProduct{Name: "Ручка", PriceCents: 100})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	wh := &cancellingWarehouse{cancel: cancel}
	obs := &observer{}
	svc := orders.NewService(store, wh, orders.Options{Observer: obs})
	_, err = svc.CreateOrder(ctx, orders.CreateOrderRequest{CustomerID: 7, Items: []orders.ItemRequest{{ProductID: pen.ID, Quantity: 1}}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if !wh.releaseCtxOK || !equal(obs.list(), []string{"reserve_failed:ok"}) {
		t.Errorf("observer = %v, release ctx alive = %v: compensation must not depend on the request context", obs.list(), wh.releaseCtxOK)
	}
}

func TestCancelReleaseFailureThenRetry(t *testing.T) {
	f := newFixture(t, nil)
	o, err := f.create(t)
	if err != nil {
		t.Fatal(err)
	}
	f.wh.Set(nil, orders.ErrWarehouseUnavailable)
	if _, err := f.svc.CancelOrder(t.Context(), o.ID); !errors.Is(err, orders.ErrWarehouseUnavailable) {
		t.Fatalf("cancel with warehouse down: %v", err)
	}
	stored, _ := f.store.Order(t.Context(), o.ID)
	if stored.Status != orders.StatusCancelled {
		t.Fatalf("status = %q: cancellation is committed before Release", stored.Status)
	}
	f.wh.Set(nil, nil)
	if _, err := f.svc.CancelOrder(t.Context(), o.ID); err != nil {
		t.Fatalf("retry cancel: %v", err)
	}
	if f.wh.Releases(o.ID) != 2 {
		t.Errorf("releases = %d, want 2", f.wh.Releases(o.ID))
	}
}

func TestCreateOrderValidation(t *testing.T) {
	f := newFixture(t, nil)
	tooMany := make([]orders.ItemRequest, orders.MaxItems+1)
	for i := range tooMany {
		tooMany[i] = orders.ItemRequest{ProductID: int64(i + 1), Quantity: 1}
	}
	tests := []struct {
		name      string
		req       orders.CreateOrderRequest
		wantField string
	}{
		{"no customer", orders.CreateOrderRequest{Items: []orders.ItemRequest{{ProductID: 1, Quantity: 1}}}, "customer_id"},
		{"no items", orders.CreateOrderRequest{CustomerID: 1}, "items"},
		{"too many items", orders.CreateOrderRequest{CustomerID: 1, Items: tooMany}, "items"},
		{"zero quantity", orders.CreateOrderRequest{CustomerID: 1, Items: []orders.ItemRequest{{ProductID: 1}}}, "items[0].quantity"},
		{"duplicate", orders.CreateOrderRequest{CustomerID: 1, Items: []orders.ItemRequest{{ProductID: 1, Quantity: 1}, {ProductID: 1, Quantity: 1}}}, "items[1].product_id"},
		{"unknown product", orders.CreateOrderRequest{CustomerID: 1, Items: []orders.ItemRequest{{ProductID: f.pen.ID, Quantity: 1}, {ProductID: 999_999, Quantity: 1}}}, "items[1].product_id"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.svc.CreateOrder(t.Context(), tt.req)
			var ve *orders.ValidationError
			if !errors.As(err, &ve) || ve.Field != tt.wantField {
				t.Fatalf("error = %v, want ValidationError on %s", err, tt.wantField)
			}
		})
	}
	if got := f.obs.list(); len(got) != 0 {
		t.Errorf("observer = %v: invalid requests must not reach the warehouse", got)
	}
}

func TestStatusCanTransitionTo(t *testing.T) {
	if !orders.StatusReserved.CanTransitionTo(orders.StatusCancelled) {
		t.Error("reserved → cancelled must be allowed")
	}
	if orders.StatusCancelled.CanTransitionTo(orders.StatusReserved) || orders.StatusCancelled.CanTransitionTo(orders.StatusCancelled) {
		t.Error("cancelled is final")
	}
}

func TestEventTypesMatchContract(t *testing.T) {
	if got := string((&eventsv1.OrderCreated{}).ProtoReflect().Descriptor().FullName()); got != orders.EventOrderCreated {
		t.Errorf("EventOrderCreated = %q, contract says %q", orders.EventOrderCreated, got)
	}
	if got := string((&eventsv1.OrderCancelled{}).ProtoReflect().Descriptor().FullName()); got != orders.EventOrderCancelled {
		t.Errorf("EventOrderCancelled = %q, contract says %q", orders.EventOrderCancelled, got)
	}
}
