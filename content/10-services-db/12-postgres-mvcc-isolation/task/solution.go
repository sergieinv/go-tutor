package task

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// Beginner — то, что нужно от пула: *pgxpool.Pool и *pgx.Conn
// подходят как есть, в тестах — фейк.
type Beginner interface {
	BeginTx(ctx context.Context, opts pgx.TxOptions) (pgx.Tx, error)
}

// ErrRetriesExhausted — попытки кончились, а конфликт остался.
var ErrRetriesExhausted = errors.New("retries exhausted")

// IsRetryable сообщает, лечится ли ошибка повтором транзакции.
func IsRetryable(err error) bool {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return false
	}
	// 40001 — serialization_failure, 40P01 — deadlock_detected.
	return pgErr.Code == "40001" || pgErr.Code == "40P01"
}

// RunInTx выполняет fn в транзакции и повторяет её целиком при
// конфликтах сериализации, но не больше maxAttempts раз.
func RunInTx(ctx context.Context, db Beginner, opts pgx.TxOptions, maxAttempts int,
	fn func(ctx context.Context, tx pgx.Tx) error) error {
	maxAttempts = max(maxAttempts, 1)
	var err error
	for range maxAttempts {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return fmt.Errorf("run in tx: %w", ctxErr)
		}
		tx, beginErr := db.BeginTx(ctx, opts)
		if beginErr != nil {
			return fmt.Errorf("begin: %w", beginErr)
		}
		// Повторяем всю транзакцию с новым снимком, а не один
		// упавший запрос: всё, что fn прочитала, могло устареть.
		err = attempt(ctx, tx, fn)
		if !IsRetryable(err) {
			return err
		}
	}
	return fmt.Errorf("%w after %d attempts: %w", ErrRetriesExhausted, maxAttempts, err)
}

// attempt — одна транзакция: выполнить и зафиксировать. defer
// откатит её при ошибке и при панике; после Commit Rollback
// ничего не делает.
func attempt(ctx context.Context, tx pgx.Tx, fn func(ctx context.Context, tx pgx.Tx) error) error {
	defer tx.Rollback(ctx)
	if err := fn(ctx, tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
