package task

import (
	"context"
	"database/sql"
	"errors"
	"slices"
	"sync"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// recorder считает запросы и запоминает последний — так тест
// видит, сколько раз ListOrders сходил в базу и с чем.
type recorder struct {
	q     Querier
	mu    sync.Mutex
	n     int
	query string
	args  []any
}

func (r *recorder) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	r.mu.Lock()
	r.n++
	r.query, r.args = query, args
	r.mu.Unlock()
	return r.q.QueryContext(ctx, query, args...)
}

// newDB — SQLite в памяти со схемой урока. Одно соединение: если
// ListOrders не закроет rows, следующий запрос не получит его.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", ":memory:")
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

func insert(t *testing.T, db *sql.DB, orders ...Order) {
	t.Helper()
	for _, o := range orders {
		_, err := db.Exec(`INSERT INTO orders (id, customer_id, created_at, total) VALUES ($1, $2, $3, $4)`,
			o.ID, o.CustomerID, o.CreatedAt, o.Total)
		if err != nil {
			t.Fatal(err)
		}
	}
}

func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func ids(orders []Order) []int64 {
	out := []int64{}
	for _, o := range orders {
		out = append(out, o.ID)
	}
	return out
}

// walk проходит ленту целиком страницами по limit.
func walk(t *testing.T, ctx context.Context, q Querier, customer int64, limit int) []int64 {
	t.Helper()
	var all []int64
	cursor := ""
	for range 1000 {
		page, err := ListOrders(ctx, q, customer, cursor, limit)
		if err != nil {
			t.Fatalf("ListOrders: %v", err)
		}
		if len(page.Orders) > limit {
			t.Fatalf("страница из %d заказов при limit %d", len(page.Orders), limit)
		}
		all = append(all, ids(page.Orders)...)
		if page.Next == "" {
			return all
		}
		if len(page.Orders) == 0 {
			t.Fatal("пустая страница с непустым Next")
		}
		cursor = page.Next
	}
	t.Fatal("лента не кончается")
	return nil
}

func TestFirstPage(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	insert(t, db,
		Order{1, 7, 100, 10}, Order{2, 7, 300, 20}, Order{3, 8, 400, 30},
		Order{4, 7, 200, 40}, Order{5, 7, 500, 50})
	page, err := ListOrders(ctx, db, 7, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	want := []Order{{5, 7, 500, 50}, {2, 7, 300, 20}, {4, 7, 200, 40}}
	if !slices.Equal(page.Orders, want) {
		t.Fatalf("Orders = %v, want %v", page.Orders, want)
	}
	if page.Next == "" {
		t.Fatal("Next пуст, а у покупателя есть ещё заказ")
	}
}

func TestWalkAll(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 7; i++ {
		insert(t, db, Order{i, 7, i * 10, i})
	}
	got := walk(t, ctx, db, 7, 2)
	want := []int64{7, 6, 5, 4, 3, 2, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("лента = %v, want %v", got, want)
	}
}

func TestBadLimit(t *testing.T) {
	db := newDB(t)
	rec := &recorder{q: db}
	for _, limit := range []int{0, -1, MaxLimit + 1} {
		if _, err := ListOrders(testCtx(t), rec, 7, "", limit); !errors.Is(err, ErrBadLimit) {
			t.Errorf("limit %d: err = %v, want ErrBadLimit", limit, err)
		}
	}
	if rec.n != 0 {
		t.Fatalf("при плохом limit было запросов: %d, want 0", rec.n)
	}
}

func TestOneQueryPerPage(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 5; i++ {
		insert(t, db, Order{i, 7, i, i})
	}
	rec := &recorder{q: db}
	page, err := ListOrders(ctx, rec, 7, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ListOrders(ctx, rec, 7, page.Next, 2); err != nil {
		t.Fatal(err)
	}
	if rec.n != 2 {
		t.Fatalf("две страницы — запросов %d, want 2", rec.n)
	}
}
