package task

import (
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func cursorOf(at, id int64) string {
	return base64.RawURLEncoding.EncodeToString(fmt.Appendf(nil, "%d:%d", at, id))
}

// Одинаковое время у многих заказов: порядок и границы страниц
// держатся на id. Курсор только по времени потерял бы строки.
func TestTiesByID(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 10; i++ {
		insert(t, db, Order{i, 7, 1000 + (i-1)/5, i}) // по 5 заказов на одно время
	}
	got := walk(t, ctx, db, 7, 3)
	want := []int64{10, 9, 8, 7, 6, 5, 4, 3, 2, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("лента = %v, want %v", got, want)
	}
}

// Новые заказы появляются между страницами — следующая страница
// продолжает с того же места: без повторов (OFFSET дал бы их) и
// без пропусков.
func TestInsertBetweenPages(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 9; i++ {
		insert(t, db, Order{i, 7, i * 10, i})
	}
	var got []int64
	cursor := ""
	next := int64(100)
	for {
		page, err := ListOrders(ctx, db, 7, cursor, 4)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ids(page.Orders)...)
		if page.Next == "" {
			break
		}
		cursor = page.Next
		insert(t, db, Order{next, 7, next * 10, 0}, Order{next + 1, 7, next * 10, 0})
		next += 2
	}
	want := []int64{9, 8, 7, 6, 5, 4, 3, 2, 1}
	if !slices.Equal(got, want) {
		t.Fatalf("лента = %v, want %v", got, want)
	}
}

// Заказ, на котором стоит курсор, удалён — лента продолжается со
// следующего старшего.
func TestDeletedCursorRow(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 6; i++ {
		insert(t, db, Order{i, 7, i * 10, i})
	}
	page, err := ListOrders(ctx, db, 7, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`DELETE FROM orders WHERE id = 4`); err != nil {
		t.Fatal(err)
	}
	page, err = ListOrders(ctx, db, 7, page.Next, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Orders); !slices.Equal(got, []int64{3, 2, 1}) {
		t.Fatalf("вторая страница = %v, want [3 2 1]", got)
	}
}

// Курсор — base64url без дополнения от «created_at:id» последнего
// заказа страницы, и позиция в нём не обязана существовать.
func TestCursorFormat(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 6; i++ {
		insert(t, db, Order{i, 7, i * 10, i})
	}
	page, err := ListOrders(ctx, db, 7, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := cursorOf(50, 5); page.Next != want {
		t.Fatalf("Next = %q, want %q (base64url «50:5»)", page.Next, want)
	}
	page, err = ListOrders(ctx, db, 7, cursorOf(35, 999), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Orders); !slices.Equal(got, []int64{3, 2, 1}) || page.Next != "" {
		t.Fatalf("после «35:999» = %v, Next %q; want [3 2 1] и пустой Next", got, page.Next)
	}
	page, err = ListOrders(ctx, db, 7, cursorOf(30, 3), 10)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Orders); !slices.Equal(got, []int64{2, 1}) {
		t.Fatalf("после «30:3» = %v, want [2 1]: заказ под курсором не повторяется", got)
	}
}

func TestBadCursor(t *testing.T) {
	db := newDB(t)
	rec := &recorder{q: db}
	bad := []string{
		"!!!",
		base64.StdEncoding.EncodeToString([]byte("10:1")) + "=", // с дополнением
		base64.RawURLEncoding.EncodeToString([]byte("101")),
		base64.RawURLEncoding.EncodeToString([]byte("10:1:2")),
		base64.RawURLEncoding.EncodeToString([]byte("x:1")),
		base64.RawURLEncoding.EncodeToString([]byte("10:")),
		base64.RawURLEncoding.EncodeToString([]byte(":1")),
		base64.RawURLEncoding.EncodeToString([]byte("1 OR 1=1:1")),
	}
	for _, c := range bad {
		if _, err := ListOrders(testCtx(t), rec, 7, c, 10); !errors.Is(err, ErrBadCursor) {
			t.Errorf("курсор %q: err = %v, want ErrBadCursor", c, err)
		}
	}
	if rec.n != 0 {
		t.Fatalf("при плохом курсоре было запросов: %d, want 0", rec.n)
	}
}

// Заказов ровно на две страницы — вторая сообщает, что дальше
// пусто, и третьей (пустой) страницы не нужно.
func TestExactPages(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 6; i++ {
		insert(t, db, Order{i, 7, i, i})
	}
	rec := &recorder{q: db}
	page, err := ListOrders(ctx, rec, 7, "", 3)
	if err != nil {
		t.Fatal(err)
	}
	page, err = ListOrders(ctx, rec, 7, page.Next, 3)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Orders); !slices.Equal(got, []int64{3, 2, 1}) || page.Next != "" {
		t.Fatalf("вторая страница = %v, Next %q; want [3 2 1] и пустой Next", got, page.Next)
	}
	if rec.n != 2 {
		t.Fatalf("запросов %d, want 2: по одному на страницу", rec.n)
	}
}

func TestEmptyAndForeign(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	insert(t, db, Order{1, 8, 10, 1}, Order{2, 9, 20, 2})
	page, err := ListOrders(ctx, db, 7, "", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Orders) != 0 || page.Next != "" {
		t.Fatalf("у покупателя без заказов: %v, Next %q", page.Orders, page.Next)
	}
	page, err = ListOrders(ctx, db, 8, "", MaxLimit)
	if err != nil {
		t.Fatal(err)
	}
	if got := ids(page.Orders); !slices.Equal(got, []int64{1}) {
		t.Fatalf("покупатель 8: %v, want [1]", got)
	}
}

// План SQLite: строки берутся из индекса по покупателю, без
// полного прохода таблицы и без сортировки во временном дереве.
func TestUsesIndex(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	for i := int64(1); i <= 50; i++ {
		insert(t, db, Order{i, i % 5, i, i})
	}
	if _, err := db.Exec(`ANALYZE`); err != nil {
		t.Fatal(err)
	}
	rec := &recorder{q: db}
	page, err := ListOrders(ctx, rec, 3, "", 4)
	if err != nil {
		t.Fatal(err)
	}
	for _, cursor := range []string{"", page.Next} {
		if _, err := ListOrders(ctx, rec, 3, cursor, 4); err != nil {
			t.Fatal(err)
		}
		rows, err := db.Query("EXPLAIN QUERY PLAN "+rec.query, rec.args...)
		if err != nil {
			t.Fatal(err)
		}
		var plan []string
		for rows.Next() {
			var id, parent, unused int
			var detail string
			if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
				t.Fatal(err)
			}
			plan = append(plan, detail)
		}
		rows.Close()
		text := strings.Join(plan, "; ")
		if strings.Contains(text, "SCAN orders") || strings.Contains(text, "TEMP B-TREE") ||
			!strings.Contains(text, "orders_customer_feed") {
			t.Errorf("курсор %q: план %q — нужен поиск по индексу orders_customer_feed без сортировки", cursor, text)
		}
	}
}

func TestContextAndConnections(t *testing.T) {
	db := newDB(t)
	insert(t, db, Order{1, 7, 10, 1}, Order{2, 7, 20, 2}, Order{3, 7, 30, 3})
	ctx := testCtx(t)
	for range 5 {
		if _, err := ListOrders(ctx, db, 7, "", 1); err != nil {
			t.Fatal(err)
		}
	}
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("после вызовов занято соединений: %d, want 0", n)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ListOrders(canceled, db, 7, "", 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("отменённый контекст: err = %v, want context.Canceled", err)
	}
}

// TestPostgres — на настоящем PostgreSQL профиля labs: та же лента
// через database/sql + pgx/stdlib, а план запроса — без Seq Scan и
// без Sort.
func TestPostgres(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN не задан — нужен профиль labs")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	ctx := testCtx(t)
	for _, q := range []string{
		`DROP TABLE IF EXISTS orders`,
		Schema,
		`INSERT INTO orders SELECT g, g % 100, 1000000 + g / 3, g FROM generate_series(1, 30000) g`,
		`VACUUM ANALYZE orders`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	var want []int64
	rows, err := db.QueryContext(ctx, `SELECT id FROM orders WHERE customer_id = 42 ORDER BY created_at DESC, id DESC LIMIT 60`)
	if err != nil {
		t.Fatal(err)
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			t.Fatal(err)
		}
		want = append(want, id)
	}
	rows.Close()

	rec := &recorder{q: db}
	var got []int64
	cursor := ""
	for range 3 {
		page, err := ListOrders(ctx, rec, 42, cursor, 20)
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, ids(page.Orders)...)
		cursor = page.Next
		var raw []byte
		if err := db.QueryRowContext(ctx, "EXPLAIN (FORMAT JSON) "+rec.query, rec.args...).Scan(&raw); err != nil {
			t.Fatal(err)
		}
		var plan []struct{ Plan pgNode }
		if err := json.Unmarshal(raw, &plan); err != nil || len(plan) == 0 {
			t.Fatalf("план: %v", err)
		}
		if bad := plan[0].Plan.find("Seq Scan", "Sort"); bad != "" {
			t.Errorf("в плане %q есть узел %s — нужен проход по индексу orders_customer_feed", rec.query, bad)
		}
	}
	if !slices.Equal(got, want) {
		t.Fatalf("три страницы = %v, want %v", got, want)
	}
}

type pgNode struct {
	Type  string   `json:"Node Type"`
	Plans []pgNode `json:"Plans"`
}

func (n pgNode) find(types ...string) string {
	if slices.Contains(types, n.Type) {
		return n.Type
	}
	for _, c := range n.Plans {
		if s := c.find(types...); s != "" {
			return s
		}
	}
	return ""
}
