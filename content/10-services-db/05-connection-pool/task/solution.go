package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
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
	// LIMIT в запросе: база не отдаёт лишнего, а цикл доходит до конца.
	rows, err := r.db.QueryContext(ctx, `SELECT id, name, tag, shelf FROM items WHERE tag = ? ORDER BY id LIMIT ?`, tag, limit)
	if err != nil {
		return nil, fmt.Errorf("items by tag: %w", err)
	}
	// Возвращает соединение в пул на любом выходе из функции.
	defer rows.Close()
	var items []Item
	for rows.Next() {
		var it Item
		if err := rows.Scan(&it.ID, &it.Name, &it.Tag, &it.Shelf); err != nil {
			return nil, fmt.Errorf("items by tag: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("items by tag: %w", err)
	}
	return items, nil
}

// Count возвращает число товаров с тегом tag.
func (r *Repo) Count(ctx context.Context, tag string) (int, error) {
	var n int
	// QueryRowContext: дедлайн и отмена запроса ограничивают и ожидание соединения.
	if err := r.db.QueryRowContext(ctx, `SELECT count(*) FROM items WHERE tag = ?`, tag).Scan(&n); err != nil {
		return 0, fmt.Errorf("count items: %w", err)
	}
	return n, nil
}

// Delete удаляет товар. Удалить несуществующий — не ошибка.
func (r *Repo) Delete(ctx context.Context, id int64) error {
	// ExecContext сам возвращает соединение и отдаёт ошибку сразу.
	if _, err := r.db.ExecContext(ctx, `DELETE FROM items WHERE id = ?`, id); err != nil {
		return fmt.Errorf("delete item %d: %w", id, err)
	}
	return nil
}

// Move переставляет товар на полку shelf и записывает перемещение в журнал moves.
func (r *Repo) Move(ctx context.Context, id int64, shelf string) error {
	if shelf == "" {
		return ErrBadShelf
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("move item %d: %w", id, err)
	}
	// Откат на любом раннем выходе; после Commit ничего не делает.
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `UPDATE items SET shelf = ? WHERE id = ?`, shelf, id)
	if err != nil {
		return fmt.Errorf("move item %d: %w", id, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("move item %d: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("move item %d: %w", id, ErrNotFound)
	}
	if _, err := tx.ExecContext(ctx, `INSERT INTO moves (item_id, shelf) VALUES (?, ?)`, id, shelf); err != nil {
		return fmt.Errorf("move item %d: %w", id, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("move item %d: %w", id, err)
	}
	return nil
}
