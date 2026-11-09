// Package storetest — контрактный набор тестов для реализаций stock.Store.
//
// Набор проверяет поведение, которое видит сервис, через stock.Service: у
// хранилища в памяти и у MySQL оно обязано совпадать до кода ошибки. Он
// запускается на memstore всегда и на MySQL при WAREHOUSE_TEST_MYSQL (см.
// store_test.go в пакетах internal/store/*).
package storetest

import (
	"context"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	"gotutor/labs/warehouse/internal/stock"
)

// NewStore возвращает пустое хранилище с остатками levels (product_id →
// available, reserved = 0). Каждый вызов — отдельная база; закрытие и
// удаление — через t.Cleanup. В базе могут быть и другие товары (демо-
// миграция), поэтому тесты берут id от 100.
type NewStore func(t *testing.T, levels map[int64]int32) stock.Store

// Run запускает весь контрактный набор.
func Run(t *testing.T, newStore NewStore) {
	tests := []struct {
		name string
		fn   func(t *testing.T, newStore NewStore)
	}{
		{"ReserveAndRelease", testReserveAndRelease},
		{"ReserveSortsItems", testReserveSortsItems},
		{"ReserveRepeatIsIdempotent", testReserveRepeatIsIdempotent},
		{"ReserveMismatch", testReserveMismatch},
		{"ReserveOutOfStockIsAtomic", testReserveOutOfStockIsAtomic},
		{"ReleaseTwice", testReleaseTwice},
		{"ReleaseWithoutReserveLeavesTombstone", testTombstone},
		{"ReserveAfterReleaseIsAborted", testReserveAfterRelease},
		{"LevelOfUnknownProductIsZero", testUnknownProduct},
		{"ConcurrentReserveSameProduct", testConcurrentReserveSameProduct},
		{"ConcurrentCrossReserves", testConcurrentCrossReserves},
		{"ConcurrentReserveAndRelease", testConcurrentReserveAndRelease},
		{"ConcurrentRepeatReserve", testConcurrentRepeatReserve},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.fn(t, newStore) })
	}
}

// CleanupContext — контекст для уборки в t.Cleanup: t.Context() к этому
// моменту уже отменён.
func CleanupContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

var now = time.Date(2026, 3, 14, 15, 9, 26, 535897932, time.UTC)

func newService(s stock.Store) *stock.Service {
	return stock.NewService(s, func() time.Time { return now }, nil)
}

func level(t *testing.T, svc *stock.Service, productID int64) stock.Level {
	t.Helper()
	l, err := svc.Level(t.Context(), productID)
	if err != nil {
		t.Fatalf("Level(%d): %v", productID, err)
	}
	return l
}

func wantLevel(t *testing.T, svc *stock.Service, productID int64, available, reserved int32) {
	t.Helper()
	want := stock.Level{ProductID: productID, Available: available, Reserved: reserved}
	if got := level(t, svc, productID); got != want {
		t.Errorf("Level(%d) = %+v, want %+v", productID, got, want)
	}
}

func reserve(t *testing.T, svc *stock.Service, orderID int64, items ...stock.Item) stock.Reservation {
	t.Helper()
	r, err := svc.Reserve(t.Context(), orderID, items)
	if err != nil {
		t.Fatalf("Reserve(%d): %v", orderID, err)
	}
	return r
}

func release(t *testing.T, svc *stock.Service, orderID int64) stock.Reservation {
	t.Helper()
	r, err := svc.Release(t.Context(), orderID)
	if err != nil {
		t.Fatalf("Release(%d): %v", orderID, err)
	}
	return r
}

func equalReservation(a, b stock.Reservation) bool {
	return a.OrderID == b.OrderID && a.Status == b.Status && slices.Equal(a.Items, b.Items)
}

func testReserveAndRelease(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 10, 102: 5}))

	got := reserve(t, svc, 1, stock.Item{ProductID: 101, Quantity: 3}, stock.Item{ProductID: 102, Quantity: 5})
	want := stock.Reservation{OrderID: 1, Status: stock.StatusReserved, Items: []stock.Item{{ProductID: 101, Quantity: 3}, {ProductID: 102, Quantity: 5}}}
	if !equalReservation(got, want) {
		t.Fatalf("Reserve = %+v, want %+v", got, want)
	}
	wantLevel(t, svc, 101, 7, 3)
	wantLevel(t, svc, 102, 0, 5)

	got = release(t, svc, 1)
	want.Status = stock.StatusReleased
	if !equalReservation(got, want) {
		t.Fatalf("Release = %+v, want %+v", got, want)
	}
	wantLevel(t, svc, 101, 10, 0)
	wantLevel(t, svc, 102, 5, 0)
}

func testReserveSortsItems(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 10, 102: 10, 103: 10}))
	got := reserve(t, svc, 1,
		stock.Item{ProductID: 103, Quantity: 1},
		stock.Item{ProductID: 101, Quantity: 2},
		stock.Item{ProductID: 102, Quantity: 3})
	if want := []stock.Item{{ProductID: 101, Quantity: 2}, {ProductID: 102, Quantity: 3}, {ProductID: 103, Quantity: 1}}; !slices.Equal(got.Items, want) {
		t.Errorf("Items = %v, want %v (by product id)", got.Items, want)
	}
	// Повтор с другим порядком позиций — тот же резерв (сравнение как
	// множества), а не MISMATCH.
	again := reserve(t, svc, 1,
		stock.Item{ProductID: 102, Quantity: 3},
		stock.Item{ProductID: 103, Quantity: 1},
		stock.Item{ProductID: 101, Quantity: 2})
	if !equalReservation(again, got) {
		t.Errorf("repeat Reserve = %+v, want %+v", again, got)
	}
	wantLevel(t, svc, 101, 8, 2)
}

func testReserveRepeatIsIdempotent(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 4}))
	first := reserve(t, svc, 7, stock.Item{ProductID: 101, Quantity: 4})
	// Остатка уже нет, но повтор — не новый резерв: он возвращает
	// существующий, а не OUT_OF_STOCK.
	second := reserve(t, svc, 7, stock.Item{ProductID: 101, Quantity: 4})
	if !equalReservation(first, second) {
		t.Errorf("repeat = %+v, want %+v", second, first)
	}
	wantLevel(t, svc, 101, 0, 4)
}

func testReserveMismatch(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 10, 102: 10}))
	reserve(t, svc, 7, stock.Item{ProductID: 101, Quantity: 2})

	for _, items := range [][]stock.Item{
		{{ProductID: 101, Quantity: 3}},
		{{ProductID: 101, Quantity: 2}, {ProductID: 102, Quantity: 1}},
		{{ProductID: 102, Quantity: 2}},
	} {
		if _, err := svc.Reserve(t.Context(), 7, items); !errors.Is(err, stock.ErrReservationMismatch) {
			t.Errorf("Reserve(%v) error = %v, want ErrReservationMismatch", items, err)
		}
	}
	wantLevel(t, svc, 101, 8, 2)
	wantLevel(t, svc, 102, 10, 0)
}

func testReserveOutOfStockIsAtomic(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 10, 102: 1, 103: 2}))

	_, err := svc.Reserve(t.Context(), 1, []stock.Item{
		{ProductID: 104, Quantity: 1}, // товара нет на складе
		{ProductID: 101, Quantity: 5}, // хватает
		{ProductID: 103, Quantity: 3}, // не хватает
		{ProductID: 102, Quantity: 2}, // не хватает
	})
	var oos *stock.OutOfStockError
	if !errors.As(err, &oos) {
		t.Fatalf("Reserve error = %v, want *OutOfStockError", err)
	}
	want := []stock.Shortage{
		{ProductID: 102, Requested: 2, Available: 1},
		{ProductID: 103, Requested: 3, Available: 2},
		{ProductID: 104, Requested: 1, Available: 0},
	}
	if !slices.Equal(oos.Shortages, want) {
		t.Errorf("Shortages = %+v, want %+v", oos.Shortages, want)
	}
	// Всё или ничего: хватавшая позиция не списана, резерв не создан.
	wantLevel(t, svc, 101, 10, 0)
	wantLevel(t, svc, 102, 1, 0)
	// После отказа тот же order_id свободен.
	reserve(t, svc, 1, stock.Item{ProductID: 101, Quantity: 5})
	wantLevel(t, svc, 101, 5, 5)
}

func testReleaseTwice(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 10}))
	reserve(t, svc, 1, stock.Item{ProductID: 101, Quantity: 4})
	first := release(t, svc, 1)
	second := release(t, svc, 1)
	if !equalReservation(first, second) {
		t.Errorf("second Release = %+v, want %+v", second, first)
	}
	wantLevel(t, svc, 101, 10, 0)
}

func testTombstone(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 10}))
	got := release(t, svc, 9)
	want := stock.Reservation{OrderID: 9, Status: stock.StatusReleased}
	if !equalReservation(got, want) {
		t.Fatalf("Release without Reserve = %+v, want tombstone %+v", got, want)
	}
	if again := release(t, svc, 9); !equalReservation(again, want) {
		t.Errorf("second Release = %+v, want %+v", again, want)
	}
}

func testReserveAfterRelease(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, map[int64]int32{101: 10}))
	release(t, svc, 9) // надгробие
	_, err := svc.Reserve(t.Context(), 9, []stock.Item{{ProductID: 101, Quantity: 1}})
	if !errors.Is(err, stock.ErrReservationReleased) {
		t.Fatalf("Reserve after tombstone error = %v, want ErrReservationReleased", err)
	}
	wantLevel(t, svc, 101, 10, 0)

	// И после настоящего резерва с отменой.
	reserve(t, svc, 10, stock.Item{ProductID: 101, Quantity: 1})
	release(t, svc, 10)
	_, err = svc.Reserve(t.Context(), 10, []stock.Item{{ProductID: 101, Quantity: 1}})
	if !errors.Is(err, stock.ErrReservationReleased) {
		t.Fatalf("Reserve after Release error = %v, want ErrReservationReleased", err)
	}
	wantLevel(t, svc, 101, 10, 0)
}

func testUnknownProduct(t *testing.T, newStore NewStore) {
	svc := newService(newStore(t, nil))
	wantLevel(t, svc, 999_999, 0, 0)
}

// runParallel запускает n горутин и ждёт их. Старт — по общему сигналу,
// чтобы горутины действительно столкнулись, а не прошли по очереди.
// Ошибки собирает вызывающий: t.Fatal из чужой горутины нельзя.
func runParallel(n int, fn func(i int)) {
	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := range n {
		wg.Go(func() {
			<-start
			fn(i)
		})
	}
	close(start)
	wg.Wait()
}

func testConcurrentReserveSameProduct(t *testing.T, newStore NewStore) {
	const (
		goroutines = 24
		available  = 7
	)
	svc := newService(newStore(t, map[int64]int32{101: available}))

	errs := make([]error, goroutines)
	runParallel(goroutines, func(i int) {
		_, errs[i] = svc.Reserve(t.Context(), int64(1000+i), []stock.Item{{ProductID: 101, Quantity: 1}})
	})

	ok := 0
	for i, err := range errs {
		var oos *stock.OutOfStockError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &oos):
		default:
			t.Errorf("Reserve(%d): unexpected error %v", 1000+i, err)
		}
	}
	if ok != available {
		t.Errorf("successful reserves = %d, want exactly %d", ok, available)
	}
	wantLevel(t, svc, 101, 0, available)
}

func testConcurrentCrossReserves(t *testing.T, newStore NewStore) {
	const goroutines = 16
	svc := newService(newStore(t, map[int64]int32{101: 12, 102: 12}))

	errs := make([]error, goroutines)
	runParallel(goroutines, func(i int) {
		// Чётные просят (101, 102), нечётные — (102, 101): без единого
		// порядка блокировок такие транзакции ловят deadlock.
		items := []stock.Item{{ProductID: 101, Quantity: 1}, {ProductID: 102, Quantity: 1}}
		if i%2 == 1 {
			items[0], items[1] = items[1], items[0]
		}
		_, errs[i] = svc.Reserve(t.Context(), int64(2000+i), items)
	})

	ok := int32(0)
	for i, err := range errs {
		var oos *stock.OutOfStockError
		switch {
		case err == nil:
			ok++
		case errors.As(err, &oos):
		default:
			t.Errorf("Reserve(%d): unexpected error %v", 2000+i, err)
		}
	}
	if ok != 12 {
		t.Errorf("successful reserves = %d, want 12", ok)
	}
	wantLevel(t, svc, 101, 12-ok, ok)
	wantLevel(t, svc, 102, 12-ok, ok)
}

func testConcurrentReserveAndRelease(t *testing.T, newStore NewStore) {
	const orders = 20
	svc := newService(newStore(t, map[int64]int32{101: 100}))

	type outcome struct{ reserveErr, releaseErr error }
	results := make([]outcome, orders)
	// Для каждого заказа Reserve и Release стартуют одновременно.
	runParallel(2*orders, func(i int) {
		orderID := int64(3000 + i/2)
		if i%2 == 0 {
			_, results[i/2].reserveErr = svc.Reserve(t.Context(), orderID, []stock.Item{{ProductID: 101, Quantity: 2}})
		} else {
			_, results[i/2].releaseErr = svc.Release(t.Context(), orderID)
		}
	})

	for i, r := range results {
		if r.releaseErr != nil {
			t.Errorf("order %d: Release: %v", 3000+i, r.releaseErr)
		}
		// Либо Reserve успел первым (и Release его отменил), либо Release
		// оставил надгробие и Reserve отклонён.
		if r.reserveErr != nil && !errors.Is(r.reserveErr, stock.ErrReservationReleased) {
			t.Errorf("order %d: Reserve: %v, want nil or ErrReservationReleased", 3000+i, r.reserveErr)
		}
	}
	// Итог согласован в обоих случаях: весь товар снова доступен.
	wantLevel(t, svc, 101, 100, 0)
}

func testConcurrentRepeatReserve(t *testing.T, newStore NewStore) {
	const goroutines = 8
	svc := newService(newStore(t, map[int64]int32{101: 10}))

	errs := make([]error, goroutines)
	got := make([]stock.Reservation, goroutines)
	// Один и тот же заказ — как клиент, повторяющий запрос по таймауту.
	runParallel(goroutines, func(i int) {
		got[i], errs[i] = svc.Reserve(t.Context(), 42, []stock.Item{{ProductID: 101, Quantity: 3}})
	})
	want := stock.Reservation{OrderID: 42, Status: stock.StatusReserved, Items: []stock.Item{{ProductID: 101, Quantity: 3}}}
	for i := range goroutines {
		if errs[i] != nil {
			t.Errorf("Reserve #%d: %v", i, errs[i])
		} else if !equalReservation(got[i], want) {
			t.Errorf("Reserve #%d = %+v, want %+v", i, got[i], want)
		}
	}
	wantLevel(t, svc, 101, 7, 3)
}
