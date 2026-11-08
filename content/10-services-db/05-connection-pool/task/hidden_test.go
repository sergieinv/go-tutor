package task

import (
	"context"
	"database/sql"
	"errors"
	"sync"
	"testing"
	"time"
)

// requireReleased проверяет, что соединение вернулось в пул: InUse == 0,
// и следующий запрос в пуле из одного соединения получает его, а не ждёт.
// Ожидание ограничено контекстом — тест не зависнет и на утечке.
func requireReleased(t *testing.T, db *sql.DB, after string) {
	t.Helper()
	if n := db.Stats().InUse; n != 0 {
		t.Errorf("после %s занято соединений: %d, want 0 — соединение не вернулось в пул", after, n)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	var n int
	if err := db.QueryRowContext(ctx, `SELECT count(*) FROM items`).Scan(&n); err != nil {
		t.Fatalf("после %s следующий запрос не дождался соединения (MaxOpenConns = 1): %v", after, err)
	}
}

func TestReviewByTagReleasesConnection(t *testing.T) {
	db := newDB(t, 1)
	repo := NewRepo(db)
	// Товаров drink три, limit 2 — выборка не дочитана до конца.
	got, err := repo.ByTag(context.Background(), "drink", 2)
	if err != nil || len(got) != 2 {
		t.Fatalf("ByTag(drink, 2) = %d товаров, %v, want 2, nil", len(got), err)
	}
	requireReleased(t, db, "ByTag(drink, 2)")
}

func TestReviewCountUsesContext(t *testing.T) {
	repo := NewRepo(newDB(t, 1))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.Count(ctx, "drink"); !errors.Is(err, context.Canceled) {
		t.Fatalf("Count с отменённым контекстом: err = %v, want context.Canceled — контекст не доходит до запроса", err)
	}
}

func TestReviewDeleteReleasesConnection(t *testing.T) {
	db := newDB(t, 1)
	repo := NewRepo(db)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := repo.Delete(ctx, 1); !errors.Is(err, context.Canceled) {
		t.Errorf("Delete с отменённым контекстом: err = %v, want context.Canceled — ошибка запроса потеряна", err)
	}
	if err := repo.Delete(context.Background(), 2); err != nil {
		t.Fatalf("Delete(2): %v", err)
	}
	requireReleased(t, db, "Delete(2)")
}

func TestReviewMoveRollsBackOnError(t *testing.T) {
	db := newDB(t, 1)
	repo := NewRepo(db)
	if err := repo.Move(context.Background(), 42, "C7"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Move(42, C7) err = %v, want ErrNotFound", err)
	}
	requireReleased(t, db, "Move(42, C7) с ошибкой ErrNotFound")
	if err := repo.Move(context.Background(), 1, ""); !errors.Is(err, ErrBadShelf) {
		t.Fatalf("Move(1, \"\") err = %v, want ErrBadShelf", err)
	}
	requireReleased(t, db, "Move(1, \"\") с ошибкой ErrBadShelf")
}

func TestCanceledContext(t *testing.T) {
	repo := NewRepo(newDB(t, 0))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := repo.ByTag(ctx, "drink", 5); !errors.Is(err, context.Canceled) {
		t.Errorf("ByTag с отменённым контекстом: err = %v, want context.Canceled", err)
	}
	if err := repo.Move(ctx, 1, "C7"); !errors.Is(err, context.Canceled) {
		t.Errorf("Move с отменённым контекстом: err = %v, want context.Canceled", err)
	}
}

func TestMoveTwiceLogsBoth(t *testing.T) {
	db := newDB(t, 0)
	repo := NewRepo(db)
	ctx := context.Background()
	for _, shelf := range []string{"C1", "C2"} {
		if err := repo.Move(ctx, 5, shelf); err != nil {
			t.Fatalf("Move(5, %s): %v", shelf, err)
		}
	}
	rows, err := db.Query(`SELECT shelf FROM moves WHERE item_id = 5 ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var got []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		got = append(got, s)
	}
	if len(got) != 2 || got[0] != "C1" || got[1] != "C2" {
		t.Fatalf("журнал перемещений товара 5 = %v, want [C1 C2]", got)
	}
}

func TestConcurrentReads(t *testing.T) {
	db := newDB(t, 2)
	repo := NewRepo(db)
	var wg sync.WaitGroup
	errs := make(chan error, 40)
	for range 20 {
		wg.Go(func() {
			if items, err := repo.ByTag(context.Background(), "food", 10); err != nil || len(items) != 2 {
				errs <- errors.New("ByTag(food, 10) вернул не 2 товара")
			}
			if n, err := repo.Count(context.Background(), "food"); err != nil || n != 2 {
				errs <- errors.New("Count(food) != 2")
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if s := db.Stats(); s.InUse != 0 || s.OpenConnections > 2 {
		t.Errorf("после параллельных чтений: InUse = %d, OpenConnections = %d, want 0 и не больше 2", s.InUse, s.OpenConnections)
	}
}
