package stock_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"

	"gotutor/labs/warehouse/internal/stock"
	"gotutor/labs/warehouse/internal/store/memstore"
)

// Поведение резерва целиком проверяет контрактный набор (internal/storetest)
// на обоих хранилищах. Здесь — то, что относится только к сервису:
// метрики и повтор fn хранилищем.

func TestMetricsCountResults(t *testing.T) {
	reg := prometheus.NewRegistry()
	svc := stock.NewService(memstore.New(map[int64]int32{1: 2}), time.Now, stock.NewMetrics(reg))
	ctx := t.Context()

	one := []stock.Item{{ProductID: 1, Quantity: 1}}
	mustReserve := func(orderID int64, items []stock.Item) error {
		_, err := svc.Reserve(ctx, orderID, items)
		return err
	}
	_ = mustReserve(1, one)                                       // reserved
	_ = mustReserve(1, one)                                       // duplicate
	_ = mustReserve(1, []stock.Item{{ProductID: 1, Quantity: 2}}) // mismatch
	_ = mustReserve(2, []stock.Item{{ProductID: 1, Quantity: 5}}) // out_of_stock
	_, _ = svc.Release(ctx, 1)                                    // released
	_, _ = svc.Release(ctx, 1)                                    // already_released
	_, _ = svc.Release(ctx, 3)                                    // tombstone
	_ = mustReserve(3, one)                                       // released (Reserve)

	expected := `
# HELP warehouse_reservations_total Reserve calls by result: reserved, duplicate, out_of_stock, released, mismatch.
# TYPE warehouse_reservations_total counter
warehouse_reservations_total{result="duplicate"} 1
warehouse_reservations_total{result="mismatch"} 1
warehouse_reservations_total{result="out_of_stock"} 1
warehouse_reservations_total{result="released"} 1
warehouse_reservations_total{result="reserved"} 1
# HELP warehouse_releases_total Release calls by result: released, already_released, tombstone.
# TYPE warehouse_releases_total counter
warehouse_releases_total{result="already_released"} 1
warehouse_releases_total{result="released"} 1
warehouse_releases_total{result="tombstone"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected),
		"warehouse_reservations_total", "warehouse_releases_total"); err != nil {
		t.Error(err)
	}
}

// retryingStore выполняет fn дважды: первая попытка «падает на deadlock»
// после того, как fn отработала, — как MySQL, откативший транзакцию при
// фиксации. Проверяет, что fn пригодна к повтору: результат — от второй
// попытки, остаток списан один раз.
type retryingStore struct {
	*memstore.Store
	attempts int
}

var errDeadlock = errors.New("deadlock")

func (s *retryingStore) InTx(ctx context.Context, fn func(ctx context.Context, tx stock.Tx) error) error {
	// Первая попытка — до конца, но с откатом.
	_ = s.Store.InTx(ctx, func(ctx context.Context, tx stock.Tx) error {
		s.attempts++
		if err := fn(ctx, tx); err != nil {
			return err
		}
		return errDeadlock // откат memstore
	})
	s.attempts++
	return s.Store.InTx(ctx, fn)
}

func TestReserveFnIsRetryable(t *testing.T) {
	store := &retryingStore{Store: memstore.New(map[int64]int32{1: 5})}
	reg := prometheus.NewRegistry()
	svc := stock.NewService(store, time.Now, stock.NewMetrics(reg))

	r, err := svc.Reserve(t.Context(), 1, []stock.Item{{ProductID: 1, Quantity: 2}})
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if store.attempts != 2 || r.Status != stock.StatusReserved {
		t.Fatalf("attempts = %d, reservation = %+v", store.attempts, r)
	}
	l, _ := svc.Level(t.Context(), 1)
	if l.Available != 3 || l.Reserved != 2 {
		t.Errorf("level = %+v, want 3 available, 2 reserved (one write)", l)
	}
	// Метрика — один раз, по итоговой попытке.
	expected := `
# HELP warehouse_reservations_total Reserve calls by result: reserved, duplicate, out_of_stock, released, mismatch.
# TYPE warehouse_reservations_total counter
warehouse_reservations_total{result="duplicate"} 0
warehouse_reservations_total{result="mismatch"} 0
warehouse_reservations_total{result="out_of_stock"} 0
warehouse_reservations_total{result="released"} 0
warehouse_reservations_total{result="reserved"} 1
`
	if err := testutil.GatherAndCompare(reg, strings.NewReader(expected), "warehouse_reservations_total"); err != nil {
		t.Error(err)
	}
}

func TestOutOfStockErrorMessage(t *testing.T) {
	err := &stock.OutOfStockError{Shortages: []stock.Shortage{
		{ProductID: 1, Requested: 3, Available: 1},
		{ProductID: 2, Requested: 1, Available: 0},
	}}
	want := "out of stock: product 1: requested 3, available 1; product 2: requested 1, available 0"
	if err.Error() != want {
		t.Errorf("Error() = %q, want %q", err.Error(), want)
	}
}

func TestStoreErrorIsWrapped(t *testing.T) {
	boom := errors.New("connection reset")
	svc := stock.NewService(failingStore{err: boom}, time.Now, nil)
	_, err := svc.Reserve(t.Context(), 1, []stock.Item{{ProductID: 1, Quantity: 1}})
	if !errors.Is(err, boom) || !strings.HasPrefix(err.Error(), "reserve: ") {
		t.Errorf("Reserve error = %v, want wrapped %v", err, boom)
	}
	if _, err := svc.Level(t.Context(), 1); !errors.Is(err, boom) {
		t.Errorf("Level error = %v, want %v", err, boom)
	}
}

type failingStore struct{ err error }

func (f failingStore) InTx(context.Context, func(context.Context, stock.Tx) error) error {
	return f.err
}
func (f failingStore) Level(context.Context, int64) (stock.Level, error) { return stock.Level{}, f.err }
