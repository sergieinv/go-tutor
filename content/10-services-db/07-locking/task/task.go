package task

import (
	"context"
	"database/sql"
	"errors"
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

var errTODO = errors.New("не реализовано")

// Get читает остаток и его версию.
func Get(ctx context.Context, db *sql.DB, id int64) (Stock, error) {
	return Stock{}, errTODO
}

// Save записывает s.Qty, только если версия в базе всё ещё s.Version.
func Save(ctx context.Context, db *sql.DB, s Stock) error {
	return errTODO
}

// TryReserve — одна попытка списать n единиц: прочитать, проверить, записать по версии.
func TryReserve(ctx context.Context, db *sql.DB, id, n int64) error {
	return errTODO
}

// Retry — политика повторов при ErrConflict.
type Retry struct {
	Attempts int
	Backoff  func(attempt int) time.Duration
	Sleep    func(ctx context.Context, d time.Duration) error
}

// Do вызывает fn, пока она возвращает ErrConflict и есть попытки.
func (r Retry) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	return errTODO
}

// Reserve списывает n единиц с повторами всей операции при конфликте версий.
func Reserve(ctx context.Context, db *sql.DB, r Retry, id, n int64) error {
	return errTODO
}

// ExpBackoff — экспоненциальная пауза с полным джиттером.
func ExpBackoff(base, maxDelay time.Duration, rnd func() float64) func(attempt int) time.Duration {
	return func(attempt int) time.Duration { return 0 }
}
