package task

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
)

func TestWithTxPanic(t *testing.T) {
	db := newDB(t)
	var got any
	func() {
		defer func() { got = recover() }()
		WithTx(testCtx(t), db, func(tx *sql.Tx) error {
			tx.Exec(`UPDATE stock SET qty = 7 WHERE sku = 'A'`)
			panic("сбой")
		})
	}()
	if got != "сбой" {
		t.Fatalf("паника в fn: recover() = %v, want \"сбой\" — WithTx должна пропустить панику дальше", got)
	}
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("после паники занято соединений: %d, want 0 — транзакция не завершена", n)
	}
	if a := stock(t, db, "A"); a != 10 {
		t.Fatalf("после паники остаток A = %d, want 10 (откат)", a)
	}
}

func TestWithTxCommitError(t *testing.T) {
	db := newDB(t)
	// fn сама откатила транзакцию — Commit вернёт sql.ErrTxDone, и его нельзя терять.
	err := WithTx(testCtx(t), db, func(tx *sql.Tx) error {
		return tx.Rollback()
	})
	if !errors.Is(err, sql.ErrTxDone) {
		t.Fatalf("WithTx при неудачном Commit: err = %v, want sql.ErrTxDone", err)
	}
}

func TestWithTxBeginError(t *testing.T) {
	db := newDB(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	called := false
	err := WithTx(ctx, db, func(tx *sql.Tx) error {
		called = true
		return nil
	})
	if !errors.Is(err, context.Canceled) || called {
		t.Fatalf("WithTx с отменённым контекстом: err = %v, fn вызвана = %v; want context.Canceled и fn не вызвана", err, called)
	}
}

func TestWithTxReleasesConnection(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	WithTx(ctx, db, func(tx *sql.Tx) error { return nil })
	WithTx(ctx, db, func(tx *sql.Tx) error { return errors.New("x") })
	if n := db.Stats().InUse; n != 0 {
		t.Fatalf("после WithTx занято соединений: %d, want 0", n)
	}
}

func TestReserveInvalid(t *testing.T) {
	db := newDB(t)
	for _, lines := range [][]Line{nil, {}, {{"A", 0}}, {{"A", -1}}, {{"", 1}}, {{"A", 1}, {"B", 0}}} {
		if err := Reserve(testCtx(t), db, 1, sql.Null[string]{}, lines); !errors.Is(err, ErrInvalid) {
			t.Errorf("Reserve(%v) err = %v, want ErrInvalid", lines, err)
		}
	}
	if n := count(t, db, "orders"); n != 0 {
		t.Errorf("после некорректных Reserve заказов %d, want 0", n)
	}
}

func TestReserveErrorOrder(t *testing.T) {
	tests := []struct {
		lines []Line
		want  error
	}{
		{[]Line{{"X", 1}, {"B", 6}}, ErrUnknownSKU},
		{[]Line{{"B", 6}, {"X", 1}}, ErrOutOfStock},
		{[]Line{{"A", 1}, {"C", 1}}, ErrOutOfStock},
		{[]Line{{"A", 11}}, ErrOutOfStock},
		{[]Line{{"A", 1}, {"a", 1}}, ErrUnknownSKU},
	}
	for _, tt := range tests {
		db := newDB(t)
		err := Reserve(testCtx(t), db, 1, sql.Null[string]{}, tt.lines)
		if !errors.Is(err, tt.want) {
			t.Errorf("Reserve(%v) err = %v, want %v", tt.lines, err, tt.want)
		}
		if a, b := stock(t, db, "A"), stock(t, db, "B"); a != 10 || b != 5 {
			t.Errorf("после Reserve(%v) остатки A, B = %d, %d, want 10, 5", tt.lines, a, b)
		}
	}
}

func TestReserveSameSKUTwice(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	if err := Reserve(ctx, db, 1, sql.Null[string]{}, []Line{{"B", 3}, {"B", 2}}); err != nil {
		t.Fatalf("Reserve B:3, B:2 при остатке 5: %v", err)
	}
	if err := Reserve(ctx, db, 2, sql.Null[string]{}, []Line{{"A", 1}, {"A", 10}}); !errors.Is(err, ErrOutOfStock) {
		t.Fatalf("Reserve A:1, A:10 при остатке 10: err = %v, want ErrOutOfStock", err)
	}
	o, err := GetOrder(ctx, db, 1)
	if err != nil || len(o.Lines) != 2 || o.Lines[0] != (Line{"B", 3}) || o.Lines[1] != (Line{"B", 2}) {
		t.Fatalf("GetOrder(1) = %+v, %v, want строки [B 3] [B 2] в порядке передачи", o, err)
	}
	if b, a := stock(t, db, "B"), stock(t, db, "A"); b != 0 || a != 10 {
		t.Fatalf("остатки B, A = %d, %d, want 0, 10", b, a)
	}
}

func TestReserveDuplicateOrder(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	if err := Reserve(ctx, db, 1, note("первый"), []Line{{"A", 1}}); err != nil {
		t.Fatal(err)
	}
	if err := Reserve(ctx, db, 1, note("второй"), []Line{{"B", 1}}); err == nil {
		t.Fatal("Reserve с занятым id заказа вернул nil, want ошибку")
	}
	if b := stock(t, db, "B"); b != 5 {
		t.Errorf("остаток B = %d, want 5", b)
	}
	o, err := GetOrder(ctx, db, 1)
	if err != nil || o.Note != note("первый") || len(o.Lines) != 1 {
		t.Errorf("GetOrder(1) = %+v, %v, want исходный заказ", o, err)
	}
}

func TestNullNote(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	if err := Reserve(ctx, db, 1, sql.Null[string]{}, []Line{{"A", 1}}); err != nil {
		t.Fatal(err)
	}
	if err := Reserve(ctx, db, 2, note(""), []Line{{"A", 1}}); err != nil {
		t.Fatal(err)
	}
	var null1, null2 bool
	db.QueryRow(`SELECT (SELECT note IS NULL FROM orders WHERE id = 1), (SELECT note IS NULL FROM orders WHERE id = 2)`).Scan(&null1, &null2)
	if !null1 || null2 {
		t.Fatalf("note IS NULL: заказ 1 — %v, заказ 2 — %v; want true (Valid=false → NULL) и false (пустая строка — не NULL)", null1, null2)
	}
	o1, _ := GetOrder(ctx, db, 1)
	o2, _ := GetOrder(ctx, db, 2)
	if o1.Note.Valid || !o2.Note.Valid || o2.Note.V != "" {
		t.Fatalf("GetOrder: Note заказа 1 = %+v, заказа 2 = %+v; want {Valid:false} и {V:\"\" Valid:true}", o1.Note, o2.Note)
	}
}

func TestGetOrderInsideTx(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	errStop := errors.New("стоп")
	var inside Order
	err := WithTx(ctx, db, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO orders (id, note) VALUES (5, NULL)`); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO reservations (order_id, sku, qty) VALUES (5, 'A', 2)`); err != nil {
			return err
		}
		var err error
		inside, err = GetOrder(ctx, tx, 5) // незафиксированное видно внутри своей транзакции
		if err != nil {
			return err
		}
		return errStop
	})
	if !errors.Is(err, errStop) {
		t.Fatalf("WithTx err = %v, want errStop", err)
	}
	if inside.ID != 5 || len(inside.Lines) != 1 || inside.Lines[0] != (Line{"A", 2}) {
		t.Fatalf("GetOrder через tx = %+v, want заказ 5 со строкой A:2", inside)
	}
	if _, err := GetOrder(ctx, db, 5); !errors.Is(err, ErrNotFound) {
		t.Fatalf("после отката GetOrder(5) err = %v, want ErrNotFound", err)
	}
}

func TestConcurrentReserve(t *testing.T) {
	db := newDB(t)
	ctx := testCtx(t)
	var wg sync.WaitGroup
	errs := make([]error, 20)
	for i := range 20 {
		wg.Go(func() {
			errs[i] = Reserve(ctx, db, int64(i+1), sql.Null[string]{}, []Line{{"A", 1}, {"B", 1}})
		})
	}
	wg.Wait()
	ok := 0
	for i, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, ErrOutOfStock):
			t.Errorf("заказ %d: err = %v, want nil или ErrOutOfStock", i+1, err)
		}
	}
	if a, b := stock(t, db, "A"), stock(t, db, "B"); ok != 5 || a != 5 || b != 0 {
		t.Errorf("успешных заказов %d, остатки A, B = %d, %d; want 5 и 5, 0", ok, a, b)
	}
	if n := count(t, db, "reservations"); n != 10 {
		t.Errorf("строк резерва %d, want 10", n)
	}
	if n := db.Stats().InUse; n != 0 {
		t.Errorf("занято соединений: %d, want 0", n)
	}
}
