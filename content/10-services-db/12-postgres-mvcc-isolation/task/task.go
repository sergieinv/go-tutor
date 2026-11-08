package task

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
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
	// TODO: *pgconn.PgError с кодом 40001 или 40P01.
	return false
}

// RunInTx выполняет fn в транзакции и повторяет её целиком при
// конфликтах сериализации, но не больше maxAttempts раз.
func RunInTx(ctx context.Context, db Beginner, opts pgx.TxOptions, maxAttempts int,
	fn func(ctx context.Context, tx pgx.Tx) error) error {
	// TODO: BeginTx, fn, Commit/Rollback, повторы.
	return nil
}
