package task

import (
	"context"
	"errors"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestRunInTxExhausted(t *testing.T) {
	for _, code := range []string{"40001", "40P01"} {
		db := &fakeDB{}
		err := RunInTx(context.Background(), db, serializable, 3, func(ctx context.Context, tx pgx.Tx) error {
			return pgErr(code)
		})
		if !errors.Is(err, ErrRetriesExhausted) {
			t.Fatalf("%s: err=%v, want ErrRetriesExhausted", code, err)
		}
		var pe *pgconn.PgError
		if !errors.As(err, &pe) || pe.Code != code {
			t.Fatalf("%s: последняя *pgconn.PgError потеряна: %v", code, err)
		}
		if len(db.txs) != 3 {
			t.Fatalf("%s: попыток %d, want 3", code, len(db.txs))
		}
	}
}

func TestRunInTxMinAttempts(t *testing.T) {
	for _, n := range []int{0, -1} {
		db := &fakeDB{}
		calls := 0
		err := RunInTx(context.Background(), db, serializable, n, func(ctx context.Context, tx pgx.Tx) error {
			calls++
			return pgErr("40001")
		})
		if calls != 1 || !errors.Is(err, ErrRetriesExhausted) {
			t.Fatalf("maxAttempts=%d: calls=%d err=%v, want 1 попытку и ErrRetriesExhausted", n, calls, err)
		}
	}
	db := &fakeDB{}
	if err := RunInTx(context.Background(), db, serializable, 0, func(context.Context, pgx.Tx) error { return nil }); err != nil || len(db.txs) != 1 {
		t.Fatalf("maxAttempts=0 без ошибок: err=%v txs=%d", err, len(db.txs))
	}
}

func TestRunInTxCommitConflict(t *testing.T) {
	db := &fakeDB{commitErrs: []error{pgErr("40001"), nil}}
	calls := 0
	err := RunInTx(context.Background(), db, serializable, 3, func(ctx context.Context, tx pgx.Tx) error {
		calls++
		return nil
	})
	if err != nil || calls != 2 || len(db.txs) != 2 || db.txs[1].committed != 1 {
		t.Fatalf("err=%v calls=%d txs=%d, want повтор после 40001 на Commit", err, calls, len(db.txs))
	}

	errDisk := errors.New("connection reset")
	db = &fakeDB{commitErrs: []error{errDisk}}
	err = RunInTx(context.Background(), db, serializable, 3, func(context.Context, pgx.Tx) error { return nil })
	if !errors.Is(err, errDisk) || len(db.txs) != 1 {
		t.Fatalf("err=%v txs=%d, want ошибку Commit без повтора", err, len(db.txs))
	}
	if errors.Is(err, ErrRetriesExhausted) {
		t.Fatal("неповторяемая ошибка не должна быть ErrRetriesExhausted")
	}
}

func TestRunInTxBeginError(t *testing.T) {
	errConn := errors.New("connection refused")
	db := &fakeDB{beginErr: errConn}
	calls := 0
	err := RunInTx(context.Background(), db, serializable, 3, func(context.Context, pgx.Tx) error {
		calls++
		return nil
	})
	if !errors.Is(err, errConn) || calls != 0 || len(db.opts) != 1 {
		t.Fatalf("err=%v calls=%d begins=%d, want ошибку BeginTx сразу", err, calls, len(db.opts))
	}
}

func TestRunInTxContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	db := &fakeDB{}
	err := RunInTx(ctx, db, serializable, 3, func(context.Context, pgx.Tx) error { return nil })
	if !errors.Is(err, context.Canceled) || len(db.opts) != 0 {
		t.Fatalf("отменённый ctx: err=%v begins=%d, want context.Canceled без BeginTx", err, len(db.opts))
	}

	ctx, cancel = context.WithCancel(context.Background())
	defer cancel()
	db = &fakeDB{}
	err = RunInTx(ctx, db, serializable, 5, func(context.Context, pgx.Tx) error {
		cancel() // клиент ушёл посреди транзакции
		return pgErr("40001")
	})
	if !errors.Is(err, context.Canceled) || len(db.txs) != 1 {
		t.Fatalf("отмена во время попытки: err=%v txs=%d, want context.Canceled и без повторов", err, len(db.txs))
	}
}

func TestRunInTxPassesOwnTx(t *testing.T) {
	db := &fakeDB{}
	var seen []pgx.Tx
	RunInTx(context.Background(), db, serializable, 3, func(ctx context.Context, tx pgx.Tx) error {
		seen = append(seen, tx)
		if len(seen) < 3 {
			return pgErr("40P01")
		}
		return nil
	})
	if len(seen) != 3 || len(db.txs) != 3 {
		t.Fatalf("вызовов %d, транзакций %d, want 3 и 3", len(seen), len(db.txs))
	}
	for i := range seen {
		if seen[i] != pgx.Tx(db.txs[i]) {
			t.Fatalf("попытка %d получила не свою транзакцию", i+1)
		}
		if db.txs[i].committed == 0 && db.txs[i].rolledBack == 0 {
			t.Fatalf("транзакция %d не завершена", i+1)
		}
	}
}

func TestRunInTxPanic(t *testing.T) {
	db := &fakeDB{}
	defer func() {
		if r := recover(); r != "boom" {
			t.Fatalf("recover() = %v, want \"boom\"", r)
		}
		if len(db.txs) != 1 || db.txs[0].rolledBack == 0 || db.txs[0].committed != 0 {
			t.Fatal("при панике транзакция должна откатиться")
		}
	}()
	RunInTx(context.Background(), db, serializable, 3, func(context.Context, pgx.Tx) error {
		panic("boom")
	})
}

// TestPostgresWriteSkew — на настоящей базе профиля labs: два врача
// одновременно снимают себя с дежурства, если дежурных двое.
// Барьер после чтения гарантирует, что первые попытки пересекутся:
// Serializable оборвёт одну, а повтор увидит одного дежурного.
func TestPostgresWriteSkew(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("POSTGRES_DSN не задан — нужен профиль labs")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := pool.Exec(ctx, `DROP TABLE IF EXISTS doctors;
		CREATE TABLE doctors (name text PRIMARY KEY, on_call bool NOT NULL);
		INSERT INTO doctors VALUES ('alice', true), ('bob', true)`); err != nil {
		t.Fatal(err)
	}

	errLast := errors.New("last doctor on call")
	var barrier sync.WaitGroup
	barrier.Add(2)
	goOff := func(name string) error {
		first := true
		return RunInTx(ctx, pool, serializable, 10, func(ctx context.Context, tx pgx.Tx) error {
			var n int
			if err := tx.QueryRow(ctx, `SELECT count(*) FROM doctors WHERE on_call`).Scan(&n); err != nil {
				return err
			}
			if first {
				first = false
				barrier.Done()
				barrier.Wait() // оба прочитали «двое» до любой записи
			}
			if n < 2 {
				return errLast
			}
			_, err := tx.Exec(ctx, `UPDATE doctors SET on_call = false WHERE name = $1`, name)
			return err
		})
	}
	errs := make([]error, 2)
	var wg sync.WaitGroup
	for i, name := range []string{"alice", "bob"} {
		wg.Go(func() { errs[i] = goOff(name) })
	}
	wg.Wait()

	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM doctors WHERE on_call`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("дежурных %d, want 1 (ошибки: %v)", n, errs)
	}
	ok, last := 0, 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case errors.Is(err, errLast):
			last++
		default:
			t.Fatalf("неожиданная ошибка: %v", err)
		}
	}
	if ok != 1 || last != 1 {
		t.Fatalf("успехов %d, отказов %d, want 1 и 1", ok, last)
	}
}
