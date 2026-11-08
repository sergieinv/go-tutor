package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

// failing — обёртка, которая возвращает ошибку на запросе номер failAt.
type failing struct {
	counter
	failAt int
	err    error
}

func (f *failing) QueryContext(ctx context.Context, q string, args ...any) (*sql.Rows, error) {
	if f.queries+1 == f.failAt {
		f.queries++
		return nil, f.err
	}
	return f.counter.QueryContext(ctx, q, args...)
}

func (f *failing) ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error) {
	if f.execs+1 == f.failAt {
		f.execs++
		return nil, f.err
	}
	return f.counter.ExecContext(ctx, q, args...)
}

// withTimeout — контекст для вызовов: если код ученика держит
// единственное соединение, следующий запрос упадёт по таймауту,
// а не повесит тест.
func withTimeout(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	t.Cleanup(cancel)
	return ctx
}

func checkNoLeak(t *testing.T, db *sql.DB) {
	t.Helper()
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("после вызова занято соединений: %d, want 0 — Rows не закрыты", n)
	}
}

func TestLoadOrdersDuplicatesAndMissing(t *testing.T) {
	db := openDB(t)
	seed(t, db)
	c := &counter{db: db}
	got, err := LoadOrders(withTimeout(t), c, []int64{2, 42, 2, 1, 1, -5})
	if err != nil {
		t.Fatalf("LoadOrders = %v", err)
	}
	want := []Order{
		{ID: 1, Customer: "Анна", Items: []Item{{"tea", 2}, {"cup", 1}}},
		{ID: 2, Customer: "Борис", Items: []Item{{"coffee", 3}}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("LoadOrders([2 42 2 1 1 -5]) = %+v, want %+v", got, want)
	}
	if c.queries > 2 {
		t.Fatalf("сделано %d запросов, want не больше 2", c.queries)
	}
	checkNoLeak(t, db)
}

func TestLoadOrdersWithoutItems(t *testing.T) {
	db := openDB(t)
	exec(t, db, `INSERT INTO orders (id, customer) VALUES (5, 'пусто'), (6, 'полно')`)
	exec(t, db, `INSERT INTO items (order_id, sku, qty) VALUES (6, 'a', 1)`)
	got, err := LoadOrders(withTimeout(t), db, []int64{5, 6})
	if err != nil || len(got) != 2 {
		t.Fatalf("LoadOrders([5 6]) = (%+v, %v)", got, err)
	}
	if got[0].ID != 5 || got[0].Customer != "пусто" || len(got[0].Items) != 0 {
		t.Errorf("заказ без позиций: %+v", got[0])
	}
	if got[1].ID != 6 || !reflect.DeepEqual(got[1].Items, []Item{{"a", 1}}) {
		t.Errorf("заказ 6: %+v", got[1])
	}

	got, err = LoadOrders(withTimeout(t), db, []int64{100, 200})
	if err != nil || len(got) != 0 {
		t.Fatalf("LoadOrders несуществующих = (%+v, %v), want пустой результат", got, err)
	}
	checkNoLeak(t, db)
}

func TestLoadOrdersManyChunks(t *testing.T) {
	db := openDB(t)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	const n = 1200
	ids := make([]int64, 0, n)
	for i := n; i >= 1; i-- { // id по убыванию: результат всё равно по возрастанию
		if _, err := tx.Exec(`INSERT INTO orders (id, customer) VALUES (?, ?)`, i, "c"+strconv.Itoa(i)); err != nil {
			t.Fatal(err)
		}
		if i%3 == 0 {
			if _, err := tx.Exec(`INSERT INTO items (order_id, sku, qty) VALUES (?, 'x', ?), (?, 'y', 1)`, i, i, i); err != nil {
				t.Fatal(err)
			}
		}
		ids = append(ids, int64(i))
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	c := &counter{db: db}
	got, err := LoadOrders(withTimeout(t), c, ids)
	if err != nil {
		t.Fatalf("LoadOrders(1200 id) = %v", err)
	}
	if len(got) != n {
		t.Fatalf("LoadOrders(1200 id) вернул %d заказов", len(got))
	}
	for i, o := range got {
		id := int64(i + 1)
		if o.ID != id || o.Customer != "c"+strconv.Itoa(i+1) {
			t.Fatalf("заказ %d: %+v, want ID %d по возрастанию", i, o, id)
		}
		var want []Item
		if id%3 == 0 {
			want = []Item{{"x", id}, {"y", 1}}
		}
		if len(want) != len(o.Items) || len(want) > 0 && !reflect.DeepEqual(o.Items, want) {
			t.Fatalf("позиции заказа %d: %+v, want %+v", id, o.Items, want)
		}
	}
	if c.maxArgs > MaxIDsPerQuery {
		t.Errorf("в одном запросе %d параметров, want не больше MaxIDsPerQuery = %d", c.maxArgs, MaxIDsPerQuery)
	}
	if c.queries > 6 {
		t.Errorf("1200 id: %d запросов, want не больше 2 × ⌈1200/500⌉ = 6", c.queries)
	}
	checkNoLeak(t, db)
}

func TestLoadOrdersPlaceholders(t *testing.T) {
	db := openDB(t)
	exec(t, db, `INSERT INTO orders (id, customer) VALUES (987654321, 'x')`)
	c := &counter{db: db}
	if _, err := LoadOrders(withTimeout(t), c, []int64{987654321}); err != nil {
		t.Fatalf("LoadOrders = %v", err)
	}
	for _, q := range c.texts {
		if strings.Contains(q, "987654321") {
			t.Fatalf("id попал в текст запроса — значения передаются только через плейсхолдеры:\n%s", q)
		}
	}
}

func TestLoadOrdersQueryError(t *testing.T) {
	boom := errors.New("сбой базы")
	for _, failAt := range []int{1, 2} {
		t.Run(fmt.Sprint(failAt), func(t *testing.T) {
			db := openDB(t)
			seed(t, db)
			f := &failing{counter: counter{db: db}, failAt: failAt, err: boom}
			got, err := LoadOrders(withTimeout(t), f, []int64{1, 2})
			if f.queries < failAt {
				return // решение одним JOIN: второго запроса нет, сбоя тоже
			}
			if !errors.Is(err, boom) {
				t.Fatalf("LoadOrders при сбое запроса %d = (%+v, %v), want ошибку с errors.Is(err, boom)", failAt, got, err)
			}
			checkNoLeak(t, db)
		})
	}
}

func TestLoadOrdersCanceled(t *testing.T) {
	db := openDB(t)
	seed(t, db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := LoadOrders(ctx, db, []int64{1}); !errors.Is(err, context.Canceled) {
		t.Fatalf("LoadOrders с отменённым контекстом = %v, want context.Canceled", err)
	}
	checkNoLeak(t, db)
}

func TestInsertItemsBoundaries(t *testing.T) {
	for _, tc := range []struct{ n, execs int }{{0, 0}, {333, 1}, {334, 2}, {999, 3}, {1001, 4}} {
		t.Run(fmt.Sprint(tc.n), func(t *testing.T) {
			db := openDB(t)
			exec(t, db, `INSERT INTO orders (id, customer) VALUES (1, 'a'), (2, 'b')`)
			exec(t, db, `INSERT INTO items (order_id, sku, qty) VALUES (2, 'чужая', 1)`)
			items := make([]Item, tc.n)
			for i := range items {
				items[i] = Item{SKU: "s" + strconv.Itoa(i), Qty: int64(i)}
			}
			c := &counter{db: db}
			if err := InsertItems(withTimeout(t), c, 1, items); err != nil {
				t.Fatalf("InsertItems = %v", err)
			}
			if c.execs != tc.execs || c.queries != 0 {
				t.Errorf("InsertItems(%d) сделал %d Exec и %d Query, want %d Exec", tc.n, c.execs, c.queries, tc.execs)
			}
			if c.maxArgs > MaxParams {
				t.Errorf("в одном INSERT %d параметров, want не больше MaxParams = %d", c.maxArgs, MaxParams)
			}
			if got := itemsOf(t, db, 1); len(got) != tc.n || tc.n > 0 && !reflect.DeepEqual(got, items) {
				t.Errorf("вставлено %d позиций в нужном порядке? len = %d", tc.n, len(got))
			}
			if got := itemsOf(t, db, 2); len(got) != 1 {
				t.Errorf("позиции заказа 2 изменились: %v", got)
			}
		})
	}
}

func TestInsertItemsPlaceholders(t *testing.T) {
	db := openDB(t)
	exec(t, db, `INSERT INTO orders (id, customer) VALUES (1, 'a')`)
	evil := Item{SKU: `x'); DELETE FROM orders; --`, Qty: 1}
	c := &counter{db: db}
	if err := InsertItems(withTimeout(t), c, 1, []Item{evil}); err != nil {
		t.Fatalf("InsertItems = %v", err)
	}
	if got := itemsOf(t, db, 1); !reflect.DeepEqual(got, []Item{evil}) {
		t.Fatalf("SKU с кавычками сохранён как %v", got)
	}
	var n int
	db.QueryRow(`SELECT count(*) FROM orders`).Scan(&n)
	if n != 1 {
		t.Fatal("SKU выполнился как SQL")
	}
}

func TestInsertItemsStopsOnError(t *testing.T) {
	db := openDB(t)
	exec(t, db, `INSERT INTO orders (id, customer) VALUES (1, 'a')`)
	boom := errors.New("сбой базы")
	f := &failing{counter: counter{db: db}, failAt: 2, err: boom}
	items := make([]Item, 1000)
	for i := range items {
		items[i] = Item{SKU: "s", Qty: 1}
	}
	err := InsertItems(withTimeout(t), f, 1, items)
	if !errors.Is(err, boom) {
		t.Fatalf("InsertItems при сбое = %v, want ошибку с errors.Is(err, boom)", err)
	}
	if f.execs != 2 {
		t.Fatalf("после сбоя на втором INSERT сделано %d Exec, want 2 — остановиться на первой ошибке", f.execs)
	}
}

func TestInsertItemsCanceled(t *testing.T) {
	db := openDB(t)
	exec(t, db, `INSERT INTO orders (id, customer) VALUES (1, 'a')`)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := InsertItems(ctx, db, 1, []Item{{"a", 1}})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("InsertItems с отменённым контекстом = %v, want context.Canceled", err)
	}
	if got := itemsOf(t, db, 1); len(got) != 0 {
		t.Fatalf("с отменённым контекстом вставлено %d позиций", len(got))
	}
}
