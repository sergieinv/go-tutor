// Package stock — домен «Склада»: остатки товара и резервы заказов.
//
// Пакет не знает ни про gRPC, ни про MySQL. Хранилище описано интерфейсом
// Store здесь же, у потребителя (в Go интерфейс объявляет тот, кто им
// пользуется, а не тот, кто реализует, — в отличие от привычной в .NET
// пары IRepository + Repository в одной сборке).
package stock

import (
	"context"
	"slices"
	"time"
)

// Status — состояние резерва.
type Status string

const (
	// StatusReserved — товар списан из доступного остатка и числится за заказом.
	StatusReserved Status = "reserved"
	// StatusReleased — резерв отменён (или «надгробие» Release без Reserve).
	StatusReleased Status = "released"
)

// Item — позиция резерва.
type Item struct {
	ProductID int64
	Quantity  int32
}

// Reservation — резерв заказа. Items упорядочены по ProductID; у
// «надгробия» позиций нет.
type Reservation struct {
	OrderID int64
	Status  Status
	Items   []Item
}

// Level — остаток товара.
type Level struct {
	ProductID int64
	// Available — можно зарезервировать.
	Available int32
	// Reserved — числится за действующими резервами.
	Reserved int32
}

// Store — хранилище, которое нужно сервису.
type Store interface {
	// InTx выполняет fn в одной транзакции: ошибка fn — откат, nil —
	// фиксация. Реализация вправе выполнить fn несколько раз (MySQL
	// повторяет транзакцию после deadlock), поэтому fn не должна иметь
	// побочных эффектов вне tx — только запоминать результат последней
	// попытки в своих переменных.
	InTx(ctx context.Context, fn func(ctx context.Context, tx Tx) error) error
	// Level возвращает остаток; неизвестный товар — нулевой остаток.
	Level(ctx context.Context, productID int64) (Level, error)
}

// Tx — операции внутри транзакции. Заблокированные строки остаются
// заблокированными до конца транзакции.
type Tx interface {
	// LockLevel читает остаток товара с блокировкой (SELECT … FOR UPDATE);
	// ok == false — строки нет.
	LockLevel(ctx context.Context, productID int64) (l Level, ok bool, err error)
	// AddLevel меняет остаток товара на дельты. Строка должна существовать.
	AddLevel(ctx context.Context, productID int64, dAvailable, dReserved int32, at time.Time) error
	// ClaimReservation вставляет резерв r, если для r.OrderID резерва ещё
	// нет (created == true), иначе блокирует и возвращает существующий.
	//
	// Почему не «прочитать с блокировкой, затем вставить»: в REPEATABLE
	// READ SELECT … FOR UPDATE по несуществующему ключу берёт блокировку
	// гэпа, а гэп над последним order_id у всех новых заказов общий.
	// Транзакции, взявшие его одновременно, взаимно блокируют друг другу
	// вставку — deadlock даже у разных заказов. Вставка такой блокировки
	// не берёт: вставки разных ключей друг другу не мешают.
	ClaimReservation(ctx context.Context, r Reservation, at time.Time) (existing Reservation, created bool, err error)
	// SetReservationStatus меняет статус резерва.
	SetReservationStatus(ctx context.Context, orderID int64, s Status, at time.Time) error
}

// sortedItems возвращает копию позиций, упорядоченную по ProductID.
func sortedItems(items []Item) []Item {
	out := slices.Clone(items)
	slices.SortFunc(out, func(a, b Item) int {
		switch {
		case a.ProductID < b.ProductID:
			return -1
		case a.ProductID > b.ProductID:
			return 1
		}
		return 0
	})
	return out
}
