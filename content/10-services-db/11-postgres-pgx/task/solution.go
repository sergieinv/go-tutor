package task

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5/pgconn"
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

// Коды SQLSTATE стабильны между версиями PostgreSQL, текст
// сообщений — нет (и зависит от lc_messages).
var codes = map[string]error{
	"23505": ErrDuplicate,  // unique_violation
	"23503": ErrForeignKey, // foreign_key_violation
	"40001": ErrRetry,      // serialization_failure
	"40P01": ErrRetry,      // deadlock_detected
	"57014": ErrCanceled,   // query_canceled
}

// Classify переводит ошибку PostgreSQL в доменную.
func Classify(err error) error {
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) {
		return err
	}
	sentinel, ok := codes[pgErr.Code]
	if !ok {
		return err
	}
	// Два %w: наверху видно доменную ошибку, а исходная
	// цепочка (и *pgconn.PgError с именем ограничения) цела.
	return fmt.Errorf("%w: %w", sentinel, err)
}

var ident = regexp.MustCompile(`^[a-z_][a-z0-9_]{0,62}$`)

// BuildUpsert строит INSERT ... ON CONFLICT для таблицы table.
func BuildUpsert(table string, cols, conflict, update []string) (string, error) {
	if len(cols) == 0 || len(conflict) == 0 {
		return "", fmt.Errorf("%w: нужны колонки и ключ конфликта", ErrInvalidUpsert)
	}
	for _, id := range slices.Concat([]string{table}, cols, conflict, update) {
		if !ident.MatchString(id) {
			return "", fmt.Errorf("%w: недопустимый идентификатор %q", ErrInvalidUpsert, id)
		}
	}
	for _, list := range [][]string{cols, conflict, update} {
		if hasDup(list) {
			return "", fmt.Errorf("%w: повтор колонки", ErrInvalidUpsert)
		}
	}
	for _, c := range conflict {
		if !slices.Contains(cols, c) {
			return "", fmt.Errorf("%w: ключ %q не вставляется", ErrInvalidUpsert, c)
		}
	}
	for _, c := range update {
		if !slices.Contains(cols, c) || slices.Contains(conflict, c) {
			return "", fmt.Errorf("%w: колонку %q нельзя обновить", ErrInvalidUpsert, c)
		}
	}

	// Значения — только плейсхолдеры. Идентификаторы
	// плейсхолдерами не передать: их проверили по allowlist
	// выше и берём в кавычки.
	var b strings.Builder
	fmt.Fprintf(&b, "INSERT INTO %s (%s) VALUES (", quote(table), quoteAll(cols))
	for i := range cols {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "$%d", i+1)
	}
	fmt.Fprintf(&b, ") ON CONFLICT (%s) ", quoteAll(conflict))
	if len(update) == 0 {
		b.WriteString("DO NOTHING")
		return b.String(), nil
	}
	b.WriteString("DO UPDATE SET ")
	for i, c := range update {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "%s = EXCLUDED.%s", quote(c), quote(c))
	}
	return b.String(), nil
}

func quote(id string) string { return `"` + id + `"` }

func quoteAll(ids []string) string {
	q := make([]string, len(ids))
	for i, id := range ids {
		q[i] = quote(id)
	}
	return strings.Join(q, ", ")
}

func hasDup(list []string) bool {
	seen := map[string]bool{}
	for _, s := range list {
		if seen[s] {
			return true
		}
		seen[s] = true
	}
	return false
}
