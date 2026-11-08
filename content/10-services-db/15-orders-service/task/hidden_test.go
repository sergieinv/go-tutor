package task

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"modernc.org/sqlite"
)

func TestOutboxEvents(t *testing.T) {
	e := newEnv(t, "sqlite")
	o := decode[Order](t, e.do("POST", "/orders", orderBody([2]int{1, 1})))
	e.do("POST", fmt.Sprintf("/orders/%d/cancel", o.ID), "")
	rows, err := e.db.Query(`SELECT type, payload FROM outbox ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var typ, payload string
		rows.Scan(&typ, &payload)
		got = append(got, typ+" "+payload)
	}
	want := fmt.Sprintf(`order.created {"order_id":%d}|order.cancelled {"order_id":%d}`, o.ID, o.ID)
	if strings.Join(got, "|") != want {
		t.Errorf("outbox = %q, want %q", got, want)
	}
}

func TestGetNotFound(t *testing.T) {
	e := newEnv(t, "sqlite")
	for _, p := range []string{"/orders/42", "/orders/abc"} {
		if rec := e.do("GET", p, ""); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s: %d, want 404", p, rec.Code)
		}
	}
}

// Сбой базы не должен раскрывать клиенту текст ошибки: SQL,
// имена таблиц и столбцов — только в журнал.
func TestReviewErrorDetails(t *testing.T) {
	e := newEnv(t, "sqlite")
	o := decode[Order](t, e.do("POST", "/orders", orderBody([2]int{1, 1})))
	e.exec(t, `DROP TABLE order_items`)
	rec := e.do("GET", fmt.Sprintf("/orders/%d", o.ID), "")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("GET при сломанной базе: %d, want 500", rec.Code)
	}
	if got := decode[map[string]string](t, rec)["error"]; got != "internal error" {
		t.Errorf("тело ответа 500: %q — наружу ушли подробности ошибки, want \"internal error\"", got)
	}
}

// Клиент ушёл (контекст запроса отменён) — заказ не создаётся.
func TestReviewRequestContext(t *testing.T) {
	e := newEnv(t, "sqlite")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	e.doCtx(ctx, "POST", "/orders", orderBody([2]int{1, 1}))
	if n, a := e.count(t, "orders"), e.available(t, 1); n != 0 || a != 5 {
		t.Errorf("при отменённом контексте запроса: заказов %d, кружек %d; want 0, 5 — контекст запроса не дошёл до базы", n, a)
	}
}

// Ошибка посреди транзакции: транзакция откатывается, соединение
// возвращается в пул.
func TestReviewRollback(t *testing.T) {
	e := newEnv(t, "sqlite")
	e.exec(t, `CREATE TRIGGER broken BEFORE INSERT ON order_items WHEN NEW.product_id = 3
		BEGIN SELECT RAISE(ABORT, 'disk full'); END`)
	rec := e.do("POST", "/orders", orderBody([2]int{1, 1}, [2]int{3, 1}))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("сбой вставки позиции: %d, want 500", rec.Code)
	}
	if inUse := e.db.Stats().InUse; inUse != 0 {
		t.Fatalf("после ошибки в транзакции занято соединений: %d, want 0 — транзакция не завершена", inUse)
	}
	if n, a := e.count(t, "orders"), e.available(t, 1); n != 0 || a != 5 {
		t.Errorf("после сбоя: заказов %d, кружек %d; want 0, 5", n, a)
	}
}

// Ошибка записи события outbox не должна теряться: без события
// заказ не создаётся.
func TestReviewOutboxError(t *testing.T) {
	e := newEnv(t, "sqlite")
	e.exec(t, `CREATE TRIGGER outbox_down BEFORE INSERT ON outbox BEGIN SELECT RAISE(ABORT, 'outbox down'); END`)
	rec := e.do("POST", "/orders", orderBody([2]int{1, 1}))
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("сбой записи в outbox: %d, want 500", rec.Code)
	}
	if n, a := e.count(t, "orders"), e.available(t, 1); n != 0 || a != 5 {
		t.Errorf("заказ без события: заказов %d, кружек %d; want 0, 5", n, a)
	}
}

// Незакрытые Rows держат соединение: битая строка в базе не
// должна приводить к утечке.
func TestReviewRowsClosed(t *testing.T) {
	e := newEnv(t, "sqlite")
	o := decode[Order](t, e.do("POST", "/orders", orderBody([2]int{1, 1}, [2]int{3, 1})))
	e.exec(t, `UPDATE order_items SET quantity = 'много' WHERE order_id = ? AND product_id = 3`, o.ID)
	rec := e.do("GET", fmt.Sprintf("/orders/%d", o.ID), "")
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("GET с битой позицией: %d, want 500", rec.Code)
	}
	if inUse := e.db.Stats().InUse; inUse != 0 {
		t.Errorf("после GET занято соединений: %d, want 0 — Rows не закрыты", inUse)
	}
}

// Резерв «прочитал остаток — записал» продаёт больше, чем есть.
func TestReviewReserveRace(t *testing.T) {
	e := newEnv(t, "sqlite")
	// Две позиции одного товара: каждая по отдельности помещается
	// в остаток (5), а вместе — нет.
	rec := e.do("POST", "/orders", orderBody([2]int{1, 3}, [2]int{1, 3}))
	if rec.Code != http.StatusConflict || e.available(t, 1) != 5 {
		t.Fatalf("заказ 3+3 кружек при остатке 5: %d, остаток %d; want 409, 5", rec.Code, e.available(t, 1))
	}

	// Конкурентные заказы: успешных ровно столько, сколько было товара.
	e.exec(t, `UPDATE products SET available = 10 WHERE id = 1`)
	var created, conflicts atomic.Int32
	var wg sync.WaitGroup
	for range 30 {
		wg.Go(func() {
			switch e.do("POST", "/orders", orderBody([2]int{1, 1})).Code {
			case http.StatusCreated:
				created.Add(1)
			case http.StatusConflict:
				conflicts.Add(1)
			}
		})
	}
	wg.Wait()
	if a := e.available(t, 1); created.Load() != 10 || conflicts.Load() != 20 || a != 0 {
		t.Errorf("30 заказов по 1 при остатке 10: создано %d, 409 — %d, остаток %d; want 10, 20, 0",
			created.Load(), conflicts.Load(), a)
	}
}

// Список заказов — фиксированное число запросов, а не 1 + N.
func TestReviewNPlusOne(t *testing.T) {
	var queries atomic.Int64
	name := registerCounting(&queries)
	e := newEnv(t, name)
	list := func() int64 {
		before := queries.Load()
		rec := e.do("GET", "/orders", "")
		if rec.Code != http.StatusOK {
			t.Fatalf("GET /orders: %d %s", rec.Code, rec.Body)
		}
		return queries.Load() - before
	}
	for range 2 {
		e.do("POST", "/orders", orderBody([2]int{3, 1}))
	}
	few := list()
	for range 6 {
		e.do("POST", "/orders", orderBody([2]int{3, 1}, [2]int{3, 2}))
	}
	many := list()
	if few != many {
		t.Errorf("GET /orders: %d запросов на 2 заказа и %d на 8 — запрос позиций на каждый заказ (N+1)", few, many)
	}
}

// Драйвер-обёртка над SQLite, который считает запросы с результатом.

var registerMu sync.Mutex
var registered int

func registerCounting(n *atomic.Int64) string {
	registerMu.Lock()
	defer registerMu.Unlock()
	registered++
	name := fmt.Sprintf("sqlite-count-%d", registered)
	sql.Register(name, countingDriver{n: n})
	return name
}

type countingDriver struct{ n *atomic.Int64 }

func (d countingDriver) Open(name string) (driver.Conn, error) {
	c, err := (&sqlite.Driver{}).Open(name)
	if err != nil {
		return nil, err
	}
	return countingConn{c, d.n}, nil
}

type countingConn struct {
	driver.Conn
	n *atomic.Int64
}

func (c countingConn) QueryContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Rows, error) {
	c.n.Add(1)
	return c.Conn.(driver.QueryerContext).QueryContext(ctx, q, args)
}

func (c countingConn) ExecContext(ctx context.Context, q string, args []driver.NamedValue) (driver.Result, error) {
	return c.Conn.(driver.ExecerContext).ExecContext(ctx, q, args)
}

func (c countingConn) BeginTx(ctx context.Context, opts driver.TxOptions) (driver.Tx, error) {
	return c.Conn.(driver.ConnBeginTx).BeginTx(ctx, opts)
}

func (c countingConn) PrepareContext(ctx context.Context, q string) (driver.Stmt, error) {
	c.n.Add(1)
	return c.Conn.(driver.ConnPrepareContext).PrepareContext(ctx, q)
}
