package task

import (
	"context"
	"database/sql"
	"errors"
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
	// TODO: keyset-пагинация одним запросом
	return Page{}, nil
}
