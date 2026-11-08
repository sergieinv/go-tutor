package task

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newDB — база в файле с пулом из одного соединения: запрос через db внутри
// транзакции здесь не пройдёт мимо неё, а дождётся таймаута контекста.
// На складе: A — 10, B — 5, C — 0.
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "shop.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO stock (sku, qty) VALUES ('A', 10), ('B', 5), ('C', 0)`); err != nil {
		t.Fatal(err)
	}
	return db
}

// testCtx — контекст с щедрым таймаутом: зависание станет ошибкой, а не вечным ожиданием.
func testCtx(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func stock(t *testing.T, db *sql.DB, sku string) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(`SELECT qty FROM stock WHERE sku = ?`, sku).Scan(&n); err != nil {
		t.Fatalf("остаток %s: %v", sku, err)
	}
	return n
}

func count(t *testing.T, db *sql.DB, table string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(`SELECT count(*) FROM ` + table).Scan(&n); err != nil {
		t.Fatalf("count %s: %v", table, err)
	}
	return n
}

func note(s string) sql.Null[string] { return sql.Null[string]{V: s, Valid: true} }

func TestWithTxCommit(t *testing.T) {
	db := newDB(t)
	err := WithTx(testCtx(t), db, func(tx *sql.Tx) error {
		_, err := tx.Exec(`UPDATE stock SET qty = 7 WHERE sku = 'A'`)
		return err
	})
	if err != nil {
		t.Fatalf("WithTx: %v", err)
	}
	if got := stock(t, db, "A"); got != 7 {
		t.Fatalf("после успешного WithTx остаток A = %d, want 7", got)
	}
}

func TestWithTxRollbackOnError(t *testing.T) {
	db := newDB(t)
	errBoom := errors.New("boom")
	err := WithTx(testCtx(t), db, func(tx *sql.Tx) error {
		if _, err := tx.Exec(`UPDATE stock SET qty = 7 WHERE sku = 'A'`); err != nil {
			return err
		}
		return errBoom
	})
	if !errors.Is(err, errBoom) {
		t.Fatalf("WithTx err = %v, want ошибку fn", err)
	}
	if got := stock(t, db, "A"); got != 10 {
		t.Fatalf("после ошибки в fn остаток A = %d, want 10 (откат)", got)
	}
}

func TestReserveAndGetOrder(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	lines := []Line{{"B", 2}, {"A", 3}}
	if err := Reserve(ctx, db, 1, note("к двери"), lines); err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if a, b := stock(t, db, "A"), stock(t, db, "B"); a != 7 || b != 3 {
		t.Fatalf("остатки A, B = %d, %d, want 7, 3", a, b)
	}
	o, err := GetOrder(ctx, db, 1)
	if err != nil {
		t.Fatalf("GetOrder(1): %v", err)
	}
	if o.ID != 1 || o.Note != note("к двери") || !slices.Equal(o.Lines, lines) {
		t.Fatalf("GetOrder(1) = %+v, want {ID:1 Note:{к двери true} Lines:%v}", o, lines)
	}
}

func TestReserveOutOfStockRollsBack(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	err := Reserve(ctx, db, 1, sql.Null[string]{}, []Line{{"A", 3}, {"B", 6}})
	if !errors.Is(err, ErrOutOfStock) {
		t.Fatalf("Reserve с B:6 при остатке 5: err = %v, want ErrOutOfStock", err)
	}
	if a := stock(t, db, "A"); a != 10 {
		t.Errorf("остаток A = %d, want 10: списание по первой строке должно откатиться", a)
	}
	if n := count(t, db, "orders") + count(t, db, "reservations"); n != 0 {
		t.Errorf("после неудачного Reserve в orders и reservations %d строк, want 0", n)
	}
}

func TestGetOrderNotFound(t *testing.T) {
	db := newDB(t)
	o, err := GetOrder(testCtx(t), db, 42)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOrder(42) err = %v, want ErrNotFound", err)
	}
	if o.ID != 0 || o.Note.Valid || o.Lines != nil {
		t.Fatalf("GetOrder(42) = %+v, want нулевой Order", o)
	}
}
