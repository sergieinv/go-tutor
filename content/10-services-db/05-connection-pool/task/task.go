package task

import (
	"context"
	"database/sql"
	"errors"
)

// Schema — схема склада. Таблицы создают тесты.
const Schema = `
CREATE TABLE items (
	id    INTEGER PRIMARY KEY,
	name  TEXT NOT NULL,
	tag   TEXT NOT NULL,
	shelf TEXT NOT NULL
);
CREATE TABLE moves (
	id      INTEGER PRIMARY KEY,
	item_id INTEGER NOT NULL,
	shelf   TEXT NOT NULL
);`

// Item — товар на складе.
type Item struct {
	ID    int64
	Name  string
	Tag   string
	Shelf string
}

var (
	ErrNotFound = errors.New("item not found")
	ErrBadLimit = errors.New("limit must be positive")
	ErrBadShelf = errors.New("shelf must not be empty")
)

// Repo — хранилище товаров поверх пула *sql.DB.
type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// ByTag возвращает не больше limit товаров с тегом tag по возрастанию id.
func (r *Repo) ByTag(ctx context.Context, tag string, limit int) ([]Item, error) {
	if limit < 1 {
		return nil, ErrBadLimit
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, tag, shelf FROM items WHERE tag = ? ORDER BY id`, tag)
	if err != nil {
		return nil, err
	}
	var items []Item
	for rows.Next() {
		if len(items) == limit {
			return items, nil
		}
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Tag, &it.Shelf); err != nil {
			return nil, err
		}
		items = append(items, it)
	}
	return items, rows.Err()
}

// Count возвращает число товаров с тегом tag.
func (r *Repo) Count(ctx context.Context, tag string) (int, error) {
	var n int
	err := r.db.QueryRow(`SELECT count(*) FROM items WHERE tag = ?`, tag).Scan(&n)
	return n, err
}

// Delete удаляет товар. Удалить несуществующий — не ошибка.
func (r *Repo) Delete(ctx context.Context, id int64) error {
	r.db.QueryRowContext(ctx, `DELETE FROM items WHERE id = ?`, id)
	return nil
}

// Move переставляет товар на полку shelf и записывает перемещение в журнал moves.
func (r *Repo) Move(ctx context.Context, id int64, shelf string) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if shelf == "" {
		return ErrBadShelf
	}
	res, err := tx.ExecContext(ctx, `UPDATE items SET shelf = ? WHERE id = ?`, shelf, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO moves (item_id, shelf) VALUES (?, ?)`, id, shelf); err != nil {
		return err
	}
	return tx.Commit()
}
