package task

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeTx — транзакция без базы: встраивает интерфейс pgx.Tx
// (остальные методы не нужны) и считает Commit и Rollback.
type fakeTx struct {
	pgx.Tx
	commitErr  error
	committed  int
	rolledBack int
}

func (t *fakeTx) Commit(context.Context) error {
	t.committed++
	return t.commitErr
}

func (t *fakeTx) Rollback(context.Context) error {
	t.rolledBack++
	if t.committed > 0 {
		return pgx.ErrTxClosed // как настоящий pgx после Commit
	}
	return nil
}

// fakeDB выдаёт транзакции; commitErrs[i] — ошибка Commit i-й.
type fakeDB struct {
	beginErr   error
	commitErrs []error
	txs        []*fakeTx
	opts       []pgx.TxOptions
}

func (d *fakeDB) BeginTx(_ context.Context, opts pgx.TxOptions) (pgx.Tx, error) {
	d.opts = append(d.opts, opts)
	if d.beginErr != nil {
		return nil, d.beginErr
	}
	tx := &fakeTx{}
	if n := len(d.txs); n < len(d.commitErrs) {
		tx.commitErr = d.commitErrs[n]
	}
	d.txs = append(d.txs, tx)
	return tx, nil
}

func pgErr(code string) error { return &pgconn.PgError{Code: code, Message: "test"} }

var serializable = pgx.TxOptions{IsoLevel: pgx.Serializable}

func TestIsRetryable(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{pgErr("40001"), true},
		{pgErr("40P01"), true},
		{fmt.Errorf("update: %w", pgErr("40001")), true},
		{pgErr("23505"), false},
		{errors.New("40001"), false},
	}
	for _, c := range cases {
		if got := IsRetryable(c.err); got != c.want {
			t.Errorf("IsRetryable(%v) = %v, want %v", c.err, got, c.want)
		}
	}
}

func TestRunInTxSuccess(t *testing.T) {
	db := &fakeDB{}
	calls := 0
	err := RunInTx(context.Background(), db, serializable, 3, func(ctx context.Context, tx pgx.Tx) error {
		calls++
		return nil
	})
	if err != nil || calls != 1 || len(db.txs) != 1 || db.txs[0].committed != 1 {
		t.Fatalf("err=%v calls=%d txs=%d, want nil, 1 вызов, 1 транзакция с Commit", err, calls, len(db.txs))
	}
	if db.opts[0] != serializable {
		t.Fatalf("BeginTx получил %+v, want %+v", db.opts[0], serializable)
	}
}

func TestRunInTxRetriesSerializationFailure(t *testing.T) {
	db := &fakeDB{}
	calls := 0
	err := RunInTx(context.Background(), db, serializable, 3, func(ctx context.Context, tx pgx.Tx) error {
		calls++
		if calls < 3 {
			return fmt.Errorf("update doctors: %w", pgErr("40001"))
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("err=%v calls=%d, want nil и 3 вызова", err, calls)
	}
	for i, tx := range db.txs[:2] {
		if tx.rolledBack == 0 || tx.committed != 0 {
			t.Fatalf("попытка %d: rollback=%d commit=%d, want откат без Commit", i+1, tx.rolledBack, tx.committed)
		}
	}
	if db.txs[2].committed != 1 {
		t.Fatal("последняя попытка не зафиксирована")
	}
}

func TestRunInTxDoesNotRetryOtherErrors(t *testing.T) {
	db := &fakeDB{}
	errLast := errors.New("last doctor on call")
	calls := 0
	err := RunInTx(context.Background(), db, serializable, 5, func(ctx context.Context, tx pgx.Tx) error {
		calls++
		return errLast
	})
	if !errors.Is(err, errLast) || calls != 1 {
		t.Fatalf("err=%v calls=%d, want errLast и 1 вызов", err, calls)
	}
	if db.txs[0].rolledBack == 0 || db.txs[0].committed != 0 {
		t.Fatal("после ошибки fn транзакция должна откатиться без Commit")
	}
}
