package task

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Schema — таблица заказов и индекс под ленту покупателя. Время
// создания — unix-миллисекунды, чтобы одна схема работала и в
// SQLite, и в PostgreSQL.
const Schema = `CREATE TABLE orders (
	id          bigint PRIMARY KEY,
	customer_id bigint NOT NULL,
	created_at  bigint NOT NULL,
	total       bigint NOT NULL
);
CREATE INDEX orders_customer_feed ON orders (customer_id, created_at DESC, id DESC);`

// MaxLimit — самая большая допустимая страница.
const MaxLimit = 100

var (
	ErrBadLimit  = errors.New("bad limit")
	ErrBadCursor = errors.New("bad cursor")
)

// Order — строка таблицы orders.
type Order struct {
	ID         int64
	CustomerID int64
	CreatedAt  int64
	Total      int64
}

// Page — страница ленты и курсор следующей ("" — дальше пусто).
type Page struct {
	Orders []Order
	Next   string
}

// Querier — то, что нужно от *sql.DB (и *sql.Tx).
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// ListOrders возвращает страницу заказов покупателя от новых к
// старым, начиная после позиции курсора.
func ListOrders(ctx context.Context, q Querier, customerID int64, cursor string, limit int) (Page, error) {
	if limit < 1 || limit > MaxLimit {
		return Page{}, ErrBadLimit
	}
	query := `SELECT id, customer_id, created_at, total FROM orders WHERE customer_id = $1`
	args := []any{customerID}
	if cursor != "" {
		at, id, err := decodeCursor(cursor)
		if err != nil {
			return Page{}, err
		}
		// Кортеж сравнивается лексикографически: сначала время,
		// при равном времени — id. Индекс отдаёт строки сразу
		// после позиции курсора.
		query += ` AND (created_at, id) < ($2, $3)`
		args = append(args, at, id)
	}
	// Одна лишняя строка отвечает на вопрос «есть ли следующая
	// страница» без второго запроса.
	query += fmt.Sprintf(` ORDER BY created_at DESC, id DESC LIMIT $%d`, len(args)+1)
	args = append(args, limit+1)

	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return Page{}, fmt.Errorf("list orders: %w", err)
	}
	defer rows.Close()
	var page Page
	for rows.Next() {
		var o Order
		if err := rows.Scan(&o.ID, &o.CustomerID, &o.CreatedAt, &o.Total); err != nil {
			return Page{}, fmt.Errorf("list orders: %w", err)
		}
		page.Orders = append(page.Orders, o)
	}
	if err := rows.Err(); err != nil {
		return Page{}, fmt.Errorf("list orders: %w", err)
	}
	if len(page.Orders) > limit {
		page.Orders = page.Orders[:limit]
		last := page.Orders[limit-1]
		page.Next = encodeCursor(last.CreatedAt, last.ID)
	}
	return page, nil
}

func encodeCursor(at, id int64) string {
	return base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("%d:%d", at, id)))
}

func decodeCursor(s string) (at, id int64, err error) {
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return 0, 0, ErrBadCursor
	}
	a, b, ok := strings.Cut(string(raw), ":")
	if !ok {
		return 0, 0, ErrBadCursor
	}
	at, err1 := strconv.ParseInt(a, 10, 64)
	id, err2 := strconv.ParseInt(b, 10, 64)
	if err1 != nil || err2 != nil {
		return 0, 0, ErrBadCursor
	}
	return at, id, nil
}
