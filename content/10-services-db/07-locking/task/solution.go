package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// Schema — остатки на складе. Таблицу создают тесты.
const Schema = `CREATE TABLE stock (
	id      INTEGER PRIMARY KEY,
	qty     INTEGER NOT NULL,
	version INTEGER NOT NULL
)`

// Stock — строка остатка вместе с версией, прочитанной из базы.
type Stock struct {
	ID      int64
	Qty     int64
	Version int64
}

var (
	ErrNotFound     = errors.New("stock not found")
	ErrInvalid      = errors.New("invalid quantity")
	ErrInsufficient = errors.New("insufficient stock")
	ErrConflict     = errors.New("version conflict")
)

// Get читает остаток и его версию.
func Get(ctx context.Context, db *sql.DB, id int64) (Stock, error) {
	s := Stock{ID: id}
	err := db.QueryRowContext(ctx, `SELECT qty, version FROM stock WHERE id = ?`, id).Scan(&s.Qty, &s.Version)
	if errors.Is(err, sql.ErrNoRows) {
		return Stock{}, fmt.Errorf("stock %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Stock{}, fmt.Errorf("stock %d: %w", id, err)
	}
	return s, nil
}

// Save записывает s.Qty, только если версия в базе всё ещё s.Version.
func Save(ctx context.Context, db *sql.DB, s Stock) error {
	res, err := db.ExecContext(ctx, `UPDATE stock SET qty = ?, version = version + 1 WHERE id = ? AND version = ?`, s.Qty, s.ID, s.Version)
	if err != nil {
		return fmt.Errorf("save stock %d: %w", s.ID, err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("save stock %d: %w", s.ID, err)
	}
	if n == 0 {
		return fmt.Errorf("save stock %d: %w", s.ID, ErrConflict)
	}
	return nil
}

// TryReserve — одна попытка списать n единиц: прочитать, проверить, записать по версии.
func TryReserve(ctx context.Context, db *sql.DB, id, n int64) error {
	if n <= 0 {
		return ErrInvalid
	}
	s, err := Get(ctx, db, id)
	if err != nil {
		return err
	}
	if s.Qty < n {
		return fmt.Errorf("stock %d: %w", id, ErrInsufficient)
	}
	s.Qty -= n
	return Save(ctx, db, s)
}

// Retry — политика повторов при ErrConflict.
type Retry struct {
	Attempts int
	Backoff  func(attempt int) time.Duration
	Sleep    func(ctx context.Context, d time.Duration) error
}

// Do вызывает fn, пока она возвращает ErrConflict и есть попытки.
func (r Retry) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	attempts := max(r.Attempts, 1)
	sleep := r.Sleep
	if sleep == nil {
		sleep = sleepCtx
	}
	var err error
	for attempt := 1; attempt <= attempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		err = fn(ctx)
		if !errors.Is(err, ErrConflict) {
			return err // успех или ошибка, которую повтор не исправит
		}
		if attempt == attempts {
			break
		}
		var d time.Duration
		if r.Backoff != nil {
			d = r.Backoff(attempt)
		}
		if err := sleep(ctx, d); err != nil {
			return err
		}
	}
	return fmt.Errorf("after %d attempts: %w", attempts, err)
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Reserve списывает n единиц с повторами всей операции при конфликте версий.
func Reserve(ctx context.Context, db *sql.DB, r Retry, id, n int64) error {
	return r.Do(ctx, func(ctx context.Context) error {
		return TryReserve(ctx, db, id, n)
	})
}

// ExpBackoff — экспоненциальная пауза с полным джиттером:
// случайная доля rnd() ∈ [0, 1) от min(base·2^(attempt-1), max).
func ExpBackoff(base, maxDelay time.Duration, rnd func() float64) func(attempt int) time.Duration {
	return func(attempt int) time.Duration {
		limit := base
		for i := 1; i < attempt && limit < maxDelay; i++ {
			limit *= 2
		}
		limit = min(limit, maxDelay)
		return time.Duration(rnd() * float64(limit))
	}
}
