package task

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// counter — обёртка над базой: считает запросы и запоминает
// максимум параметров в одном запросе.
type counter struct {
	db      *sql.DB
	queries int
	execs   int
	maxArgs int
	texts   []string
}

func (c *counter) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	c.queries++
	c.maxArgs = max(c.maxArgs, len(args))
	c.texts = append(c.texts, q)
	return c.db.QueryContext(ctx, q, args...)
}

func (c *counter) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	c.execs++
	c.maxArgs = max(c.maxArgs, len(args))
	c.texts = append(c.texts, q)
	return c.db.ExecContext(ctx, q, args...)
}

// openDB открывает базу с пулом из одного соединения: запрос,
// оставивший Rows открытыми, заблокирует следующий.
func openDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() { db.Close() })
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	return db
}

func exec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatal(err)
	}
}

// itemsOf читает позиции заказа в порядке вставки.
func itemsOf(t *testing.T, db *sql.DB, orderID int64) []Item {
	t.Helper()
	rows, err := db.Query(`SELECT sku, qty FROM items WHERE order_id = ? ORDER BY id`, orderID)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.SKU, &it.Qty); err != nil {
			t.Fatal(err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	return items
}

func seed(t *testing.T, db *sql.DB) {
	t.Helper()
	exec(t, db, `INSERT INTO orders (id, customer) VALUES (1, 'Анна'), (2, 'Борис'), (3, 'Вера')`)
	exec(t, db, `INSERT INTO items (order_id, sku, qty) VALUES (1, 'tea', 2), (3, 'jam', 1), (1, 'cup', 1), (2, 'coffee', 3)`)
}

func TestLoadOrders(t *testing.T) {
	db := openDB(t)
	seed(t, db)
	c := &counter{db: db}
	got, err := LoadOrders(context.Background(), c, []int64{3, 1})
	if err != nil {
		t.Fatalf("LoadOrders = %v", err)
	}
	want := []Order{
		{ID: 1, Customer: "Анна", Items: []Item{{"tea", 2}, {"cup", 1}}},
		{ID: 3, Customer: "Вера", Items: []Item{{"jam", 1}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadOrders([3 1]) = %+v, want %+v", got, want)
	}
	if c.queries > 2 {
		t.Fatalf("LoadOrders сделал %d запросов, want не больше 2:\n%s", c.queries, strings.Join(c.texts, "\n"))
	}
}

func TestLoadOrdersEmpty(t *testing.T) {
	db := openDB(t)
	seed(t, db)
	c := &counter{db: db}
	got, err := LoadOrders(context.Background(), c, nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("LoadOrders(nil) = (%v, %v), want пустой результат", got, err)
	}
	if c.queries != 0 {
		t.Fatalf("LoadOrders(nil) сделал %d запросов, want 0", c.queries)
	}
}

func TestInsertItems(t *testing.T) {
	for _, tc := range []struct{ n, execs int }{{1, 1}, {5, 1}, {1000, 4}} {
		t.Run(fmt.Sprint(tc.n), func(t *testing.T) {
			db := openDB(t)
			exec(t, db, `INSERT INTO orders (id, customer) VALUES (7, 'опт')`)
			items := make([]Item, tc.n)
			for i := range items {
				items[i] = Item{SKU: fmt.Sprintf("sku-%d", i), Qty: int64(i + 1)}
			}
			c := &counter{db: db}
			if err := InsertItems(context.Background(), c, 7, items); err != nil {
				t.Fatalf("InsertItems = %v", err)
			}
			if c.execs != tc.execs {
				t.Errorf("InsertItems(%d позиций) сделал %d запросов, want %d", tc.n, c.execs, tc.execs)
			}
			if got := itemsOf(t, db, 7); !reflect.DeepEqual(got, items) {
				t.Fatalf("после вставки позиции заказа 7 не совпадают со вставленными (len %d, want %d)", len(got), len(items))
			}
		})
	}
}
