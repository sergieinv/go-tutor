package task

import (
	"context"
	"database/sql"
	"errors"
)

// Schema — схема склада и заказов. Таблицы создают тесты.
const Schema = `
CREATE TABLE stock (
	sku TEXT PRIMARY KEY,
	qty INTEGER NOT NULL CHECK (qty >= 0)
);
CREATE TABLE orders (
	id   INTEGER PRIMARY KEY,
	note TEXT
);
CREATE TABLE reservations (
	id       INTEGER PRIMARY KEY,
	order_id INTEGER NOT NULL,
	sku      TEXT NOT NULL,
	qty      INTEGER NOT NULL
);`

// DBTX — общее у *sql.DB и *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// Line — строка заказа: сколько единиц товара зарезервировать.
type Line struct {
	SKU string
	Qty int64
}

// Order — заказ с комментарием (NULL — комментария нет) и строками.
type Order struct {
	ID    int64
	Note  sql.Null[string]
	Lines []Line
}

var (
	ErrInvalid    = errors.New("invalid order")
	ErrUnknownSKU = errors.New("unknown sku")
	ErrOutOfStock = errors.New("out of stock")
	ErrNotFound   = errors.New("order not found")
)

var errTODO = errors.New("не реализовано")

// WithTx выполняет fn в транзакции: nil — Commit, ошибка или паника — Rollback.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	return errTODO
}

// Reserve создаёт заказ и резервирует товар по всем строкам — всё или ничего.
func Reserve(ctx context.Context, db *sql.DB, orderID int64, note sql.Null[string], lines []Line) error {
	return errTODO
}

// GetOrder читает заказ через *sql.DB или *sql.Tx.
func GetOrder(ctx context.Context, q DBTX, id int64) (Order, error) {
	return Order{}, errTODO
}
