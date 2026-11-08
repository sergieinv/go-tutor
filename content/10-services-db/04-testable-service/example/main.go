// Склад с двумя реализациями хранилища: фейк в памяти для
// быстрых тестов сервиса и SQL-реализация для настоящей базы.
// Обе обязаны вести себя одинаково — это проверяет общий
// контрактный набор тестов в `main_test.go`.

package stock

import (
	"context"
	"database/sql"
	"errors"
	"sync"
)

var (
	ErrNotFound     = errors.New("stock: sku not found")
	ErrInsufficient = errors.New("stock: insufficient quantity")
)

// Интерфейс объявляет потребитель (сервис заказов); здесь он
// рядом для краткости.
type Store interface {
	Add(ctx context.Context, sku string, qty int) error
	Take(ctx context.Context, sku string, qty int) error
	Qty(ctx context.Context, sku string) (int, error)
}

// Фейк — рабочая реализация, а не мок с ожиданиями. В проекте
// он живёт в пакете вроде `stocktest`, чтобы его могли брать
// тесты других пакетов. Отмену контекста он уважает так же,
// как настоящая база, — иначе тесты на отмену врали бы.
type MemStore struct {
	mu  sync.Mutex
	qty map[string]int
}

func NewMemStore() *MemStore { return &MemStore{qty: map[string]int{}} }

func (m *MemStore) Add(ctx context.Context, sku string, qty int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.qty[sku] += qty
	return nil
}

func (m *MemStore) Take(ctx context.Context, sku string, qty int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	have, ok := m.qty[sku]
	switch {
	case !ok:
		return ErrNotFound
	case have < qty:
		return ErrInsufficient
	}
	m.qty[sku] = have - qty
	return nil
}

func (m *MemStore) Qty(ctx context.Context, sku string) (int, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	have, ok := m.qty[sku]
	if !ok {
		return 0, ErrNotFound
	}
	return have, nil
}

// SQL-реализация принимает `DBTX` — общее у `*sql.DB` и
// `*sql.Tx`. Так тест может отдать ей транзакцию и откатить
// её в конце. SQL — общий для SQLite и PostgreSQL: оба
// понимают `$1` и `ON CONFLICT ... DO UPDATE`.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

const Schema = `CREATE TABLE IF NOT EXISTS stock (
	sku TEXT PRIMARY KEY,
	qty INTEGER NOT NULL CHECK (qty >= 0))`

type SQLStore struct{ db DBTX }

func NewSQLStore(db DBTX) *SQLStore { return &SQLStore{db: db} }

func (s *SQLStore) Add(ctx context.Context, sku string, qty int) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO stock (sku, qty) VALUES ($1, $2)
		ON CONFLICT (sku) DO UPDATE SET qty = stock.qty + excluded.qty`, sku, qty)
	return err
}

func (s *SQLStore) Take(ctx context.Context, sku string, qty int) error {
	res, err := s.db.ExecContext(ctx, `UPDATE stock SET qty = qty - $2 WHERE sku = $1 AND qty >= $2`, sku, qty)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 1 {
		return err
	}
	// Ничего не обновили: товара нет или его мало.
	if _, err := s.Qty(ctx, sku); err != nil {
		return err
	}
	return ErrInsufficient
}

func (s *SQLStore) Qty(ctx context.Context, sku string) (int, error) {
	var qty int
	err := s.db.QueryRowContext(ctx, `SELECT qty FROM stock WHERE sku = $1`, sku).Scan(&qty)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, ErrNotFound
	}
	return qty, err
}
