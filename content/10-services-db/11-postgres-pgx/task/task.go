package task

import (
	"errors"
)

// Доменные ошибки хранилища: слой выше знает о них, а не о
// кодах PostgreSQL.
var (
	ErrDuplicate     = errors.New("duplicate")
	ErrForeignKey    = errors.New("foreign key violation")
	ErrRetry         = errors.New("retryable conflict")
	ErrCanceled      = errors.New("query canceled")
	ErrInvalidUpsert = errors.New("invalid upsert")
)

// Classify переводит ошибку PostgreSQL в доменную.
func Classify(err error) error {
	// TODO: errors.As на *pgconn.PgError и разбор Code.
	return err
}

// BuildUpsert строит INSERT ... ON CONFLICT для таблицы table.
func BuildUpsert(table string, cols, conflict, update []string) (string, error) {
	// TODO: проверить идентификаторы и собрать текст запроса.
	return "", nil
}
