// Один набор проверок — три хранилища: фейк, SQLite в памяти
// и настоящий PostgreSQL. Если фейк разойдётся с базой,
// упадёт его прогон контракта, а не прод.

package stock_test

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib" // драйвер "pgx" для database/sql
	_ "modernc.org/sqlite"

	stock "lesson"
)

// Хелпер: ошибка подготовки данных — сразу `Fatal` с местом
// вызова в тесте, а не загадочный провал проверки ниже.
func add(t *testing.T, s stock.Store, sku string, qty int) {
	t.Helper()
	if err := s.Add(t.Context(), sku, qty); err != nil {
		t.Fatalf("Add(%s, %d) = %v", sku, qty, err)
	}
}

// Контракт: функция получает фабрику, а не готовое хранилище,
// — каждый подтест начинает с пустого состояния.
func testStore(t *testing.T, newStore func(t *testing.T) stock.Store) {
	t.Run("add accumulates", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "lamp", 2)
		add(t, s, "lamp", 3)
		if got, err := s.Qty(t.Context(), "lamp"); got != 5 || err != nil {
			t.Errorf("Qty(lamp) = (%d, %v), want (5, nil)", got, err)
		}
	})
	t.Run("take", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "lamp", 5)
		if err := s.Take(t.Context(), "lamp", 5); err != nil {
			t.Fatalf("Take(lamp, 5) = %v", err)
		}
		if got, _ := s.Qty(t.Context(), "lamp"); got != 0 {
			t.Errorf("Qty(lamp) после Take = %d, want 0", got)
		}
	})
	// Ошибочный вызов не меняет состояние — это тоже контракт.
	t.Run("insufficient", func(t *testing.T) {
		s := newStore(t)
		add(t, s, "lamp", 1)
		if err := s.Take(t.Context(), "lamp", 2); !errors.Is(err, stock.ErrInsufficient) {
			t.Errorf("Take(lamp, 2) = %v, want ErrInsufficient", err)
		}
		if got, _ := s.Qty(t.Context(), "lamp"); got != 1 {
			t.Errorf("Qty(lamp) = %d, want 1", got)
		}
	})
	t.Run("unknown sku", func(t *testing.T) {
		s := newStore(t)
		if _, err := s.Qty(t.Context(), "ghost"); !errors.Is(err, stock.ErrNotFound) {
			t.Errorf("Qty(ghost) = %v, want ErrNotFound", err)
		}
		if err := s.Take(t.Context(), "ghost", 1); !errors.Is(err, stock.ErrNotFound) {
			t.Errorf("Take(ghost) = %v, want ErrNotFound", err)
		}
	})
	t.Run("canceled context", func(t *testing.T) {
		s := newStore(t)
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		if err := s.Add(ctx, "lamp", 1); !errors.Is(err, context.Canceled) {
			t.Errorf("Add с отменённым контекстом = %v, want context.Canceled", err)
		}
	})
}

// Фейк проходит тот же контракт — значит, тестам сервиса на
// нём можно верить.
func TestMemStore(t *testing.T) {
	testStore(t, func(t *testing.T) stock.Store { return stock.NewMemStore() })
}

// SQLite в памяти — своя база на каждый подтест. С `:memory:`
// пулу нужно одно соединение: у каждого соединения своя база.
func TestSQLiteStore(t *testing.T) {
	testStore(t, func(t *testing.T) stock.Store {
		db, err := sql.Open("sqlite", ":memory:")
		if err != nil {
			t.Fatal(err)
		}
		db.SetMaxOpenConns(1)
		t.Cleanup(func() { db.Close() })
		if _, err := db.Exec(stock.Schema); err != nil {
			t.Fatal(err)
		}
		return stock.NewSQLStore(db)
	})
}

// PostgreSQL — по DSN из окружения. Нет базы — тест
// пропускается, а не падает: `go test ./...` работает и без
// Docker. Схема создаётся один раз, а каждый подтест работает
// в своей транзакции, которую `t.Cleanup` откатывает.
func TestPostgresStore(t *testing.T) {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		t.Skip("нужен PostgreSQL из профиля labs: docker compose --profile labs up")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	if _, err := db.ExecContext(t.Context(), stock.Schema); err != nil {
		t.Fatal(err)
	}
	testStore(t, func(t *testing.T) stock.Store {
		// Не t.Context(): он отменяется перед Cleanup, а отмена
		// контекста BeginTx сама откатывает транзакцию.
		tx, err := db.BeginTx(context.Background(), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { tx.Rollback() })
		return stock.NewSQLStore(tx)
	})
}
