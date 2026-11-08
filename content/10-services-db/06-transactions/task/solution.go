package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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

// WithTx выполняет fn в транзакции: nil — Commit, ошибка или паника — Rollback.
func WithTx(ctx context.Context, db *sql.DB, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	// Выполнится и при ошибке, и при панике в fn (паника полетит дальше);
	// после успешного Commit ничего не делает.
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// Reserve создаёт заказ и резервирует товар по всем строкам — всё или ничего.
func Reserve(ctx context.Context, db *sql.DB, orderID int64, note sql.Null[string], lines []Line) error {
	if len(lines) == 0 {
		return ErrInvalid
	}
	for _, l := range lines {
		if l.SKU == "" || l.Qty <= 0 {
			return ErrInvalid
		}
	}
	return WithTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO orders (id, note) VALUES (?, ?)`, orderID, note); err != nil {
			return fmt.Errorf("order %d: %w", orderID, err)
		}
		for _, l := range lines {
			if err := take(ctx, tx, l); err != nil {
				return fmt.Errorf("order %d: %w", orderID, err)
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO reservations (order_id, sku, qty) VALUES (?, ?, ?)`, orderID, l.SKU, l.Qty); err != nil {
				return fmt.Errorf("order %d: %w", orderID, err)
			}
		}
		return nil
	})
}

// take списывает товар одним атомарным UPDATE; 0 строк — разбираемся почему.
func take(ctx context.Context, q DBTX, l Line) error {
	res, err := q.ExecContext(ctx, `UPDATE stock SET qty = qty - ? WHERE sku = ? AND qty >= ?`, l.Qty, l.SKU, l.Qty)
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 1 {
		return nil
	}
	var one int
	err = q.QueryRowContext(ctx, `SELECT 1 FROM stock WHERE sku = ?`, l.SKU).Scan(&one)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		return fmt.Errorf("%s: %w", l.SKU, ErrUnknownSKU)
	case err != nil:
		return err
	default:
		return fmt.Errorf("%s: %w", l.SKU, ErrOutOfStock)
	}
}

// GetOrder читает заказ через *sql.DB или *sql.Tx.
func GetOrder(ctx context.Context, q DBTX, id int64) (Order, error) {
	o := Order{ID: id}
	err := q.QueryRowContext(ctx, `SELECT note FROM orders WHERE id = ?`, id).Scan(&o.Note)
	if errors.Is(err, sql.ErrNoRows) {
		return Order{}, fmt.Errorf("order %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Order{}, err
	}
	rows, err := q.QueryContext(ctx, `SELECT sku, qty FROM reservations WHERE order_id = ? ORDER BY id`, id)
	if err != nil {
		return Order{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var l Line
		if err := rows.Scan(&l.SKU, &l.Qty); err != nil {
			return Order{}, err
		}
		o.Lines = append(o.Lines, l)
	}
	if err := rows.Err(); err != nil {
		return Order{}, err
	}
	return o, nil
}
