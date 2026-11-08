package task

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newDB — база в файле с пулом из одного соединения. Строки: id 1 — 10 шт.
// (версия 1), id 2 — 0 шт. (версия 7).
func newDB(t *testing.T) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(t.TempDir(), "stock.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(Schema); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`INSERT INTO stock (id, qty, version) VALUES (1, 10, 1), (2, 0, 7)`); err != nil {
		t.Fatal(err)
	}
	return db
}

func row(t *testing.T, db *sql.DB, id int64) (qty, version int64) {
	t.Helper()
	if err := db.QueryRow(`SELECT qty, version FROM stock WHERE id = ?`, id).Scan(&qty, &version); err != nil {
		t.Fatalf("строка %d: %v", id, err)
	}
	return qty, version
}

// noSleep — Sleep для тестов: запоминает паузы и не ждёт.
type noSleep struct{ got []time.Duration }

func (s *noSleep) Sleep(ctx context.Context, d time.Duration) error {
	s.got = append(s.got, d)
	return ctx.Err()
}

func TestGetSave(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	s, err := Get(ctx, db, 1)
	if err != nil || s != (Stock{ID: 1, Qty: 10, Version: 1}) {
		t.Fatalf("Get(1) = %+v, %v, want {1 10 1}, nil", s, err)
	}
	s.Qty = 4
	if err := Save(ctx, db, s); err != nil {
		t.Fatalf("Save(%+v): %v", s, err)
	}
	if qty, ver := row(t, db, 1); qty != 4 || ver != 2 {
		t.Fatalf("после Save: qty %d, version %d, want 4, 2", qty, ver)
	}
	// s устарел: версия в базе уже 2.
	s.Qty = 9
	if err := Save(ctx, db, s); !errors.Is(err, ErrConflict) {
		t.Fatalf("Save устаревшей версии: err = %v, want ErrConflict", err)
	}
	if qty, ver := row(t, db, 1); qty != 4 || ver != 2 {
		t.Fatalf("после конфликта: qty %d, version %d, want 4, 2 без изменений", qty, ver)
	}
}

func TestTryReserve(t *testing.T) {
	db := newDB(t)
	ctx := context.Background()
	if err := TryReserve(ctx, db, 1, 3); err != nil {
		t.Fatalf("TryReserve(1, 3): %v", err)
	}
	if err := TryReserve(ctx, db, 1, 8); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("TryReserve(1, 8) при остатке 7: err = %v, want ErrInsufficient", err)
	}
	if qty, ver := row(t, db, 1); qty != 7 || ver != 2 {
		t.Fatalf("остаток %d, версия %d, want 7, 2", qty, ver)
	}
}

func TestRetryStopsOnSuccess(t *testing.T) {
	s := &noSleep{}
	calls := 0
	r := Retry{Attempts: 5, Backoff: func(a int) time.Duration { return time.Duration(a) * time.Second }, Sleep: s.Sleep}
	err := r.Do(context.Background(), func(ctx context.Context) error {
		calls++
		if calls < 3 {
			return ErrConflict
		}
		return nil
	})
	if err != nil || calls != 3 {
		t.Fatalf("Do: err = %v, вызовов %d, want nil и 3", err, calls)
	}
	if len(s.got) != 2 || s.got[0] != time.Second || s.got[1] != 2*time.Second {
		t.Fatalf("паузы %v, want [1s 2s]", s.got)
	}
}

func TestReserve(t *testing.T) {
	db := newDB(t)
	s := &noSleep{}
	r := Retry{Attempts: 3, Sleep: s.Sleep}
	if err := Reserve(context.Background(), db, r, 1, 10); err != nil {
		t.Fatalf("Reserve(1, 10): %v", err)
	}
	if err := Reserve(context.Background(), db, r, 1, 1); !errors.Is(err, ErrInsufficient) {
		t.Fatalf("Reserve(1, 1) при нулевом остатке: err = %v, want ErrInsufficient", err)
	}
	if len(s.got) != 0 {
		t.Fatalf("без конфликтов пауз быть не должно, были %v", s.got)
	}
}

func TestExpBackoff(t *testing.T) {
	half := func() float64 { return 0.5 }
	b := ExpBackoff(10*time.Millisecond, 100*time.Millisecond, half)
	want := map[int]time.Duration{1: 5 * time.Millisecond, 2: 10 * time.Millisecond, 3: 20 * time.Millisecond, 4: 40 * time.Millisecond, 5: 50 * time.Millisecond}
	for attempt, w := range want {
		if got := b(attempt); got != w {
			t.Errorf("ExpBackoff(10ms, 100ms, 0.5)(%d) = %v, want %v", attempt, got, w)
		}
	}
}
