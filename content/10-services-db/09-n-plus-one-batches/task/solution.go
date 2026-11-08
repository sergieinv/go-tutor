package task

import (
	"context"
	"database/sql"
	"fmt"
	"slices"
	"strings"
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
	ids = slices.Clone(ids)
	slices.Sort(ids)
	ids = slices.Compact(ids)
	var orders []Order
	for chunk := range slices.Chunk(ids, MaxIDsPerQuery) {
		part, err := loadChunk(ctx, db, chunk)
		if err != nil {
			return nil, err
		}
		orders = append(orders, part...)
	}
	return orders, nil
}

func loadChunk(ctx context.Context, db Querier, ids []int64) ([]Order, error) {
	args := make([]any, len(ids))
	for i, id := range ids {
		args[i] = id
	}
	in := placeholders(len(ids))

	orders, err := queryOrders(ctx, db, `SELECT id, customer FROM orders WHERE id IN (`+in+`) ORDER BY id`, args)
	if err != nil || len(orders) == 0 {
		return orders, err
	}
	byID := make(map[int64]int, len(orders))
	for i, o := range orders {
		byID[o.ID] = i
	}
	rows, err := db.QueryContext(ctx, `SELECT order_id, sku, qty FROM items WHERE order_id IN (`+in+`) ORDER BY id`, args...)
	if err != nil {
		return nil, fmt.Errorf("позиции заказов: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var it Item
		if err := rows.Scan(&id, &it.SKU, &it.Qty); err != nil {
			return nil, fmt.Errorf("позиции заказов: %w", err)
		}
		o := &orders[byID[id]]
		o.Items = append(o.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("позиции заказов: %w", err)
	}
	return orders, nil
}

func queryOrders(ctx context.Context, db Querier, q string, args []any) ([]Order, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("заказы: %w", err)
	}
	defer rows.Close()
	var orders []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.Customer); err != nil {
			return nil, fmt.Errorf("заказы: %w", err)
		}
		orders = append(orders, o)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("заказы: %w", err)
	}
	return orders, nil
}

// InsertItems вставляет позиции заказа многострочными INSERT
// пачками по MaxParams/3 строк.
func InsertItems(ctx context.Context, db Execer, orderID int64, items []Item) error {
	const perRow = 3
	for chunk := range slices.Chunk(items, MaxParams/perRow) {
		args := make([]any, 0, len(chunk)*perRow)
		for _, it := range chunk {
			args = append(args, orderID, it.SKU, it.Qty)
		}
		q := `INSERT INTO items (order_id, sku, qty) VALUES ` +
			strings.TrimSuffix(strings.Repeat("(?, ?, ?), ", len(chunk)), ", ")
		if _, err := db.ExecContext(ctx, q, args...); err != nil {
			return fmt.Errorf("вставка позиций заказа %d: %w", orderID, err)
		}
	}
	return nil
}

// placeholders(3) == "?, ?, ?"
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}
