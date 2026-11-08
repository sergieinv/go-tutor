package task

import (
	"context"
	"database/sql"
	"errors"

	"modernc.org/sqlite"
)

var (
	ErrInvalid      = errors.New("invalid argument")
	ErrNotFound     = errors.New("not found")
	ErrDuplicate    = errors.New("duplicate name")
	ErrInsufficient = errors.New("insufficient stock")
)

type Item struct {
	ID   int64
	Name string
	Qty  int
}

// Store — хранилище товаров. Его контракт проверяют общие тесты.
type Store interface {
	Create(ctx context.Context, name string, qty int) (int64, error)
	Get(ctx context.Context, id int64) (Item, error)
	Reserve(ctx context.Context, id int64, qty int) error
	List(ctx context.Context) ([]Item, error)
}

// ---- SQLite-реализация (дана, не меняйте) ----

const Schema = `CREATE TABLE items (
	id   INTEGER PRIMARY KEY AUTOINCREMENT,
	name TEXT NOT NULL UNIQUE,
	qty  INTEGER NOT NULL CHECK (qty >= 0)
)`

type SQLiteStore struct {
	db *sql.DB
}

func NewSQLiteStore(db *sql.DB) *SQLiteStore {
	return &SQLiteStore{db: db}
}

func (s *SQLiteStore) Create(ctx context.Context, name string, qty int) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	if name == "" || qty < 0 {
		return 0, ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO items (name, qty) VALUES (?, ?)`, name, qty)
	var se *sqlite.Error
	if errors.As(err, &se) && se.Code() == 2067 { // SQLITE_CONSTRAINT_UNIQUE
		return 0, ErrDuplicate
	}
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *SQLiteStore) Get(ctx context.Context, id int64) (Item, error) {
	var it Item
	err := s.db.QueryRowContext(ctx, `SELECT id, name, qty FROM items WHERE id = ?`, id).Scan(&it.ID, &it.Name, &it.Qty)
	if errors.Is(err, sql.ErrNoRows) {
		return Item{}, ErrNotFound
	}
	if err != nil {
		return Item{}, err
	}
	return it, nil
}

func (s *SQLiteStore) Reserve(ctx context.Context, id int64, qty int) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if qty <= 0 {
		return ErrInvalid
	}
	res, err := s.db.ExecContext(ctx, `UPDATE items SET qty = qty - ? WHERE id = ? AND qty >= ?`, qty, id, qty)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil || n == 1 {
		return err
	}
	if _, err := s.Get(ctx, id); err != nil {
		return err
	}
	return ErrInsufficient
}

func (s *SQLiteStore) List(ctx context.Context) ([]Item, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, qty FROM items ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Qty); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// ---- Фейк в памяти: напишите его ----

// MemStore — хранилище в памяти с тем же поведением, что у SQLiteStore.
type MemStore struct {
	// TODO: поля
}

func NewMemStore() *MemStore {
	// TODO
	return &MemStore{}
}

func (m *MemStore) Create(ctx context.Context, name string, qty int) (int64, error) {
	// TODO
	return 0, nil
}

func (m *MemStore) Get(ctx context.Context, id int64) (Item, error) {
	// TODO
	return Item{}, nil
}

func (m *MemStore) Reserve(ctx context.Context, id int64, qty int) error {
	// TODO
	return nil
}

func (m *MemStore) List(ctx context.Context) ([]Item, error) {
	// TODO
	return nil, nil
}
