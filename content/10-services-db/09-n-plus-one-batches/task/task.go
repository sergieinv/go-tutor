package task

import (
	"context"
	"database/sql"
	"errors"
)

// Schema — таблицы заказов и позиций; создают тесты.
const Schema = `
CREATE TABLE orders (
	id       INTEGER PRIMARY KEY,
	customer TEXT NOT NULL
);
CREATE TABLE items (
	id       INTEGER PRIMARY KEY,
	order_id INTEGER NOT NULL REFERENCES orders (id),
	sku      TEXT NOT NULL,
	qty      INTEGER NOT NULL
);
CREATE INDEX items_order_idx ON items (order_id);`

const (
	// MaxIDsPerQuery — сколько id можно передать в одном IN (...).
	MaxIDsPerQuery = 500
	// MaxParams — сколько параметров можно передать в одном INSERT.
	MaxParams = 999
)

// Querier и Execer — то, что нужно функциям от базы. Их реализуют
// *sql.DB, *sql.Tx и обёртки тестов, которые считают запросы.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

type Execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

type Item struct {
	SKU string
	Qty int64
}

type Order struct {
	ID       int64
	Customer string
	Items    []Item
}

// LoadOrders загружает заказы с позициями: не больше двух запросов
// на каждую пачку из MaxIDsPerQuery разных id.
func LoadOrders(ctx context.Context, db Querier, ids []int64) ([]Order, error) {
	// Сейчас это N+1: заказ за заказом, и позиции каждого — отдельным запросом.
	// Перепишите без запросов в цикле.
	var orders []Order
	for _, id := range ids {
		var o Order
		rows, err := db.QueryContext(ctx, `SELECT id, customer FROM orders WHERE id = ?`, id)
		if err != nil {
			return nil, err
		}
		found := rows.Next()
		if found {
			err = rows.Scan(&o.ID, &o.Customer)
		}
		rows.Close()
		if err != nil {
			return nil, err
		}
		if !found {
			continue
		}
		items, err := db.QueryContext(ctx, `SELECT sku, qty FROM items WHERE order_id = ? ORDER BY id`, id)
		if err != nil {
			return nil, err
		}
		for items.Next() {
			var it Item
			if err := items.Scan(&it.SKU, &it.Qty); err != nil {
				items.Close()
				return nil, err
			}
			o.Items = append(o.Items, it)
		}
		items.Close()
		orders = append(orders, o)
	}
	return orders, nil
}

// InsertItems вставляет позиции заказа многострочными INSERT
// пачками по MaxParams/3 строк.
func InsertItems(ctx context.Context, db Execer, orderID int64, items []Item) error {
	// TODO: многострочный INSERT пачками
	return errors.New("не реализовано")
}
