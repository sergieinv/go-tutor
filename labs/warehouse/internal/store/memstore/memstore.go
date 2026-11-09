// Package memstore — хранилище склада в памяти: для юнит-тестов и для
// публичного пакета warehousetest (поддельный «Склад» в тестах «Заказов»).
//
// Транзакции честные, но простые: InTx держит один мьютекс всё время
// выполнения fn (полная сериализация — уровень SERIALIZABLE без
// конкуренции), работает с копией состояния и подменяет состояние копией
// только при успехе. Ошибка fn — копия выбрасывается, это и есть откат.
package memstore

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync"
	"time"

	"gotutor/labs/warehouse/internal/stock"
)

// Store реализует stock.Store.
type Store struct {
	mu    sync.Mutex
	state state
}

type state struct {
	levels       map[int64]stock.Level
	reservations map[int64]stock.Reservation
}

var _ stock.Store = (*Store)(nil)

// New создаёт хранилище с начальными остатками: product_id → available.
func New(available map[int64]int32) *Store {
	s := &Store{state: state{
		levels:       make(map[int64]stock.Level, len(available)),
		reservations: make(map[int64]stock.Reservation),
	}}
	for id, n := range available {
		s.state.levels[id] = stock.Level{ProductID: id, Available: n}
	}
	return s
}

// clone копирует состояние. Позиции резервов не копируются: резерв в
// карте только заменяется целиком, срез Items никогда не меняется на месте.
func (st state) clone() state {
	return state{levels: maps.Clone(st.levels), reservations: maps.Clone(st.reservations)}
}

// InTx выполняет fn под мьютексом на копии состояния.
func (s *Store) InTx(ctx context.Context, fn func(ctx context.Context, tx stock.Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx := &tx{state: s.state.clone()}
	if err := fn(ctx, tx); err != nil {
		return err
	}
	s.state = tx.state
	return nil
}

// Level возвращает остаток; неизвестный товар — нулевой.
func (s *Store) Level(ctx context.Context, productID int64) (stock.Level, error) {
	if err := ctx.Err(); err != nil {
		return stock.Level{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	l, ok := s.state.levels[productID]
	if !ok {
		l = stock.Level{ProductID: productID}
	}
	return l, nil
}

// Reservation возвращает резерв заказа — для проверок в тестах.
func (s *Store) Reservation(orderID int64) (stock.Reservation, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.state.reservations[orderID]
	r.Items = slices.Clone(r.Items)
	return r, ok
}

// SetLevel задаёт остаток товара (вставка или замена) — для подготовки
// данных в тестах.
func (s *Store) SetLevel(productID int64, available int32) {
	s.mu.Lock()
	defer s.mu.Unlock()
	l := s.state.levels[productID]
	l.ProductID, l.Available = productID, available
	s.state.levels[productID] = l
}

type tx struct {
	state state
}

func (t *tx) LockLevel(_ context.Context, productID int64) (stock.Level, bool, error) {
	l, ok := t.state.levels[productID]
	return l, ok, nil
}

func (t *tx) AddLevel(_ context.Context, productID int64, dAvailable, dReserved int32, _ time.Time) error {
	l, ok := t.state.levels[productID]
	if !ok {
		return fmt.Errorf("stock of product %d not found", productID)
	}
	l.Available += dAvailable
	l.Reserved += dReserved
	// Та же защита, что CHECK (available >= 0) в схеме MySQL.
	if l.Available < 0 || l.Reserved < 0 {
		return fmt.Errorf("stock of product %d would become negative", productID)
	}
	t.state.levels[productID] = l
	return nil
}

func (t *tx) ClaimReservation(_ context.Context, r stock.Reservation, _ time.Time) (stock.Reservation, bool, error) {
	if existing, ok := t.state.reservations[r.OrderID]; ok {
		return existing, false, nil
	}
	r.Items = slices.Clone(r.Items)
	t.state.reservations[r.OrderID] = r
	return stock.Reservation{}, true, nil
}

func (t *tx) SetReservationStatus(_ context.Context, orderID int64, s stock.Status, _ time.Time) error {
	r, ok := t.state.reservations[orderID]
	if !ok {
		return fmt.Errorf("reservation %d not found", orderID)
	}
	r.Status = s
	t.state.reservations[orderID] = r
	return nil
}
