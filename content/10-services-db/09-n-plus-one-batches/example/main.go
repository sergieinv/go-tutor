// N+1: один запрос за списком и ещё по запросу на каждый
// элемент. Сравним три загрузчика заказов с позициями и
// посчитаем запросы обёрткой над `DBTX`.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	_ "modernc.org/sqlite"
)

// DBTX — то, что нужно загрузчикам. Его реализуют `*sql.DB`,
// `*sql.Tx` и счётчик ниже — тест подменяет базу на него.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// countingDB считает запросы и максимум параметров в одном.
type countingDB struct {
	DBTX
	queries, maxArgs int
}

func (c *countingDB) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.queries++
	c.maxArgs = max(c.maxArgs, len(args))
	return c.DBTX.QueryContext(ctx, q, args...)
}

func (c *countingDB) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.queries++
	c.maxArgs = max(c.maxArgs, len(args))
	return c.DBTX.ExecContext(ctx, q, args...)
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

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "orders")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "orders.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	seed(ctx, db)

	// Три загрузчика возвращают одно и то же — разница в
	// числе запросов: 1+N, 2 и 1.
	var first []Order
	for _, l := range []struct {
		name string
		load func(context.Context, DBTX) ([]Order, error)
	}{{"N+1", loadNPlusOne}, {"IN", loadIn}, {"JOIN", loadJoin}} {
		c := &countingDB{DBTX: db}
		orders, err := l.load(ctx, c)
		if err != nil {
			log.Fatal(err)
		}
		if first == nil {
			first = orders
		}
		fmt.Printf("%-4s запросов: %d, совпадает: %v\n", l.name, c.queries, reflect.DeepEqual(orders, first))
	}
	for _, o := range first {
		fmt.Println(" ", o.ID, o.Customer, o.Items)
	}

	// Пакетная вставка: 25 000 строк по 3 параметра. Один
	// `INSERT` на всё — 75 000 параметров, больше лимита SQLite.
	items := make([]Item, 25_000)
	for i := range items {
		items[i] = Item{SKU: fmt.Sprintf("sku-%d", i%100), Qty: 1}
	}
	_, err = db.ExecContext(ctx, insertSQL(len(items)), itemArgs(4, items)...)
	fmt.Println("одним запросом:", err)

	// Пачки по `maxParams / 3` строк — в одной транзакции,
	// чтобы вставка была атомарной (и быстрой: один fsync).
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	defer tx.Rollback()
	c := &countingDB{DBTX: tx}
	if err := insertItems(ctx, c, 4, items); err != nil {
		log.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
	var n int
	db.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE order_id = 4`).Scan(&n)
	fmt.Printf("пачками: запросов %d, макс. параметров %d, строк %d\n", c.queries, c.maxArgs, n)
}

// loadNPlusOne — запрос в цикле: на 1000 заказов это 1001
// запрос и столько же сетевых round trip до базы.
func loadNPlusOne(ctx context.Context, db DBTX) ([]Order, error) {
	orders, err := loadOrders(ctx, db)
	if err != nil {
		return nil, err
	}
	for i := range orders {
		rows, err := db.QueryContext(ctx, `SELECT sku, qty FROM items WHERE order_id = ? ORDER BY id`, orders[i].ID)
		if err != nil {
			return nil, err
		}
		orders[i].Items, err = scanItems(rows)
		if err != nil {
			return nil, err
		}
	}
	return orders, nil
}

// loadIn — два запроса при любом N: заказы, затем все их
// позиции через `IN (?, ?, ...)`. Плейсхолдеров столько же,
// сколько id; сами значения в текст SQL не попадают.
func loadIn(ctx context.Context, db DBTX) ([]Order, error) {
	orders, err := loadOrders(ctx, db)
	if err != nil || len(orders) == 0 {
		return orders, err // `IN ()` в PostgreSQL и MySQL — синтаксическая ошибка
	}
	byID := make(map[int64]int, len(orders)) // id → индекс в срезе
	args := make([]any, len(orders))
	for i, o := range orders {
		byID[o.ID] = i
		args[i] = o.ID
	}
	q := `SELECT order_id, sku, qty FROM items WHERE order_id IN (` + placeholders(len(args)) + `) ORDER BY order_id, id`
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var it Item
		if err := rows.Scan(&id, &it.SKU, &it.Qty); err != nil {
			return nil, err
		}
		o := &orders[byID[id]]
		o.Items = append(o.Items, it)
	}
	return orders, rows.Err()
}

// loadJoin — один запрос. `LEFT JOIN` сохраняет заказы без
// позиций (у них `sku` — `NULL`), а данные заказа повторяются
// в каждой строке: при широких заказах трафик растёт.
func loadJoin(ctx context.Context, db DBTX) ([]Order, error) {
	rows, err := db.QueryContext(ctx, `SELECT o.id, o.customer, i.sku, i.qty
		FROM orders o LEFT JOIN items i ON i.order_id = o.id
		ORDER BY o.id, i.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orders []Order
	for rows.Next() {
		var o Order
		var sku sql.NullString
		var qty sql.NullInt64
		if err := rows.Scan(&o.ID, &o.Customer, &sku, &qty); err != nil {
			return nil, err
		}
		if n := len(orders); n == 0 || orders[n-1].ID != o.ID {
			orders = append(orders, o)
		}
		if sku.Valid {
			last := &orders[len(orders)-1]
			last.Items = append(last.Items, Item{SKU: sku.String, Qty: qty.Int64})
		}
	}
	return orders, rows.Err()
}

// insertItems режет строки на пачки (`slices.Chunk`, Go 1.23)
// так, чтобы в одном запросе было не больше `maxParams`
// параметров.
func insertItems(ctx context.Context, db DBTX, orderID int64, items []Item) error {
	const maxParams, perRow = 32766, 3 // лимит SQLite с версии 3.32
	for chunk := range slices.Chunk(items, maxParams/perRow) {
		if _, err := db.ExecContext(ctx, insertSQL(len(chunk)), itemArgs(orderID, chunk)...); err != nil {
			return fmt.Errorf("вставка позиций: %w", err)
		}
	}
	return nil
}

func insertSQL(rows int) string {
	return `INSERT INTO items (order_id, sku, qty) VALUES ` +
		strings.TrimSuffix(strings.Repeat("(?, ?, ?), ", rows), ", ")
}

func itemArgs(orderID int64, items []Item) []any {
	args := make([]any, 0, len(items)*3)
	for _, it := range items {
		args = append(args, orderID, it.SKU, it.Qty)
	}
	return args
}

// placeholders(3) == "?, ?, ?"
func placeholders(n int) string {
	return strings.TrimSuffix(strings.Repeat("?, ", n), ", ")
}

func loadOrders(ctx context.Context, db DBTX) ([]Order, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, customer FROM orders ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var orders []Order
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.Customer); err != nil {
			return nil, err
		}
		orders = append(orders, o)
	}
	return orders, rows.Err()
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.SKU, &it.Qty); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

func seed(ctx context.Context, db *sql.DB) {
	for _, q := range []string{
		`CREATE TABLE orders (id INTEGER PRIMARY KEY, customer TEXT NOT NULL)`,
		`CREATE TABLE items (id INTEGER PRIMARY KEY, order_id INTEGER NOT NULL REFERENCES orders (id), sku TEXT NOT NULL, qty INTEGER NOT NULL)`,
		`CREATE INDEX items_order_idx ON items (order_id)`,
		`INSERT INTO orders (id, customer) VALUES (1, 'Анна'), (2, 'Борис'), (3, 'Вера'), (4, 'Глеб')`,
		`INSERT INTO items (order_id, sku, qty) VALUES (1, 'tea', 2), (1, 'cup', 1), (2, 'coffee', 3), (3, 'tea', 1), (3, 'jam', 2), (3, 'cup', 4)`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			log.Fatal(err)
		}
	}
}
