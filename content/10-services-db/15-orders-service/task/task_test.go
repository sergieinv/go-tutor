package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// env — сервис на SQLite-файле во временном каталоге. Транзакции
// берут блокировку записи сразу (_txlock=immediate) и ждут друг
// друга до 10 с (busy_timeout), а не падают с «database is locked».
type env struct {
	db *sql.DB
	h  http.Handler
}

func newEnv(t *testing.T, driver string) *env {
	t.Helper()
	dsn := filepath.Join(t.TempDir(), "orders.db") + "?_pragma=busy_timeout(10000)&_txlock=immediate"
	db, err := sql.Open(driver, dsn)
	if err != nil {
		t.Fatal(err)
	}
	db.SetMaxOpenConns(8)
	t.Cleanup(func() { db.Close() })
	s, err := New(db)
	if err != nil {
		t.Fatal(err)
	}
	if s == nil {
		t.Fatal("New вернула nil")
	}
	e := &env{db: db, h: s.Handler()}
	e.exec(t, `INSERT INTO products (id, name, available) VALUES (1, 'кружка', 5), (2, 'чайник', 1), (3, 'ложка', 100)`)
	return e
}

func (e *env) exec(t *testing.T, q string, args ...any) {
	t.Helper()
	if _, err := e.db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// do вызывает обработчик напрямую, с контекстом ctx.
func (e *env) doCtx(ctx context.Context, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequestWithContext(ctx, method, path, strings.NewReader(body))
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, req)
	return rec
}

func (e *env) do(method, path, body string) *httptest.ResponseRecorder {
	return e.doCtx(context.Background(), method, path, body)
}

func (e *env) available(t *testing.T, id int64) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var n int
	if err := e.db.QueryRowContext(ctx, `SELECT available FROM products WHERE id = ?`, id).Scan(&n); err != nil {
		t.Fatalf("остаток товара %d: %v", id, err)
	}
	return n
}

func (e *env) count(t *testing.T, table string) int {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var n int
	if err := e.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM `+table).Scan(&n); err != nil {
		t.Fatalf("COUNT(*) FROM %s: %v", table, err)
	}
	return n
}

func decode[T any](t *testing.T, rec *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(rec.Body.Bytes(), &v); err != nil {
		t.Fatalf("ответ %d не JSON: %q", rec.Code, rec.Body.String())
	}
	return v
}

func orderBody(items ...[2]int) string {
	var parts []string
	for _, it := range items {
		parts = append(parts, fmt.Sprintf(`{"product_id":%d,"quantity":%d}`, it[0], it[1]))
	}
	return `{"items":[` + strings.Join(parts, ",") + `]}`
}

func TestCreateAndGet(t *testing.T) {
	e := newEnv(t, "sqlite")
	rec := e.do("POST", "/orders", orderBody([2]int{1, 2}, [2]int{2, 1}))
	if rec.Code != http.StatusCreated {
		t.Fatalf("POST /orders: %d %s", rec.Code, rec.Body)
	}
	o := decode[Order](t, rec)
	if o.ID == 0 || o.Status != "reserved" || len(o.Items) != 2 || o.Items[0] != (Item{1, 2}) || o.Items[1] != (Item{2, 1}) {
		t.Errorf("создан %+v", o)
	}
	if a, b := e.available(t, 1), e.available(t, 2); a != 3 || b != 0 {
		t.Errorf("остатки %d, %d; want 3, 0", a, b)
	}
	got := decode[Order](t, e.do("GET", fmt.Sprintf("/orders/%d", o.ID), ""))
	if got.ID != o.ID || got.Status != "reserved" || len(got.Items) != 2 || got.Items[0] != (Item{1, 2}) {
		t.Errorf("GET вернул %+v", got)
	}
}

func TestOutOfStockAndUnknown(t *testing.T) {
	e := newEnv(t, "sqlite")
	rec := e.do("POST", "/orders", orderBody([2]int{1, 1}, [2]int{2, 2}))
	if rec.Code != http.StatusConflict || decode[map[string]string](t, rec)["error"] != "out of stock" {
		t.Errorf("не хватает чайников: %d %s", rec.Code, rec.Body)
	}
	rec = e.do("POST", "/orders", orderBody([2]int{1, 1}, [2]int{9, 1}))
	if rec.Code != http.StatusUnprocessableEntity || decode[map[string]string](t, rec)["error"] != "unknown product" {
		t.Errorf("неизвестный товар: %d %s", rec.Code, rec.Body)
	}
	if a := e.available(t, 1); a != 5 || e.count(t, "orders") != 0 {
		t.Errorf("после отказов: кружек %d, заказов %d; want 5, 0", a, e.count(t, "orders"))
	}
}

func TestValidation(t *testing.T) {
	e := newEnv(t, "sqlite")
	for _, body := range []string{
		`{"items":`, `{"items":[]}`, `{}`,
		orderBody([2]int{1, 0}), orderBody([2]int{1, 1001}), orderBody([2]int{1, -1}),
	} {
		if rec := e.do("POST", "/orders", body); rec.Code != http.StatusBadRequest {
			t.Errorf("POST %s: %d, want 400", body, rec.Code)
		}
	}
	big := `{"items":[` + strings.Repeat(`{"product_id":3,"quantity":1},`, 3000) + `{"product_id":3,"quantity":1}]}`
	if rec := e.do("POST", "/orders", big); rec.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("тело > 64 КиБ: %d, want 413", rec.Code)
	}
	if n := e.count(t, "orders"); n != 0 {
		t.Errorf("заказов %d, want 0", n)
	}
}

func TestCancel(t *testing.T) {
	e := newEnv(t, "sqlite")
	o := decode[Order](t, e.do("POST", "/orders", orderBody([2]int{1, 2}, [2]int{3, 10})))
	rec := e.do("POST", fmt.Sprintf("/orders/%d/cancel", o.ID), "")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel: %d %s", rec.Code, rec.Body)
	}
	if got := decode[Order](t, rec); got.Status != "cancelled" || len(got.Items) != 2 {
		t.Errorf("cancel вернул %+v", got)
	}
	if a, b := e.available(t, 1), e.available(t, 3); a != 5 || b != 100 {
		t.Errorf("после отмены остатки %d, %d; want 5, 100", a, b)
	}
	if rec := e.do("POST", fmt.Sprintf("/orders/%d/cancel", o.ID), ""); rec.Code != http.StatusConflict {
		t.Errorf("повторная отмена: %d, want 409", rec.Code)
	}
	if rec := e.do("POST", "/orders/999/cancel", ""); rec.Code != http.StatusNotFound {
		t.Errorf("отмена несуществующего: %d, want 404", rec.Code)
	}
}

func TestList(t *testing.T) {
	e := newEnv(t, "sqlite")
	if got := decode[[]Order](t, e.do("GET", "/orders", "")); got == nil || len(got) != 0 {
		t.Errorf("пустой список: %v, want []", got)
	}
	e.do("POST", "/orders", orderBody([2]int{1, 1}))
	e.do("POST", "/orders", orderBody([2]int{3, 2}, [2]int{1, 1}))
	got := decode[[]Order](t, e.do("GET", "/orders", ""))
	if len(got) != 2 || got[0].ID >= got[1].ID || len(got[0].Items) != 1 || len(got[1].Items) != 2 || got[1].Items[0] != (Item{3, 2}) {
		t.Errorf("GET /orders = %+v", got)
	}
}
