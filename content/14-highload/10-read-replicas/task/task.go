package task

import (
	"context"
	"errors"
)

// ErrBadLSN — строка не похожа на LSN PostgreSQL.
var ErrBadLSN = errors.New("bad lsn")

// LSN — позиция в журнале WAL: старшие 32 бита до «/», младшие — после.
type LSN uint64

// ParseLSN разбирает LSN в текстовом виде PostgreSQL: "16/B374D848".
func ParseLSN(s string) (LSN, error) {
	// TODO
	return 0, nil
}

// String печатает LSN так же, как PostgreSQL.
func (l LSN) String() string {
	// TODO
	return ""
}

// Order — заказ в истории покупателя.
type Order struct {
	ID    int64
	Total int64
}

// Primary — основной сервер: на нём всегда последние данные.
type Primary interface {
	History(ctx context.Context, customerID int64) ([]Order, error)
}

// Replica — реплика: данные на ней могут отставать.
type Replica interface {
	History(ctx context.Context, customerID int64) ([]Order, error)
	ReplayLSN(ctx context.Context) (LSN, error)
}

// Router направляет чтение истории на реплики с учётом токена.
type Router struct {
	// TODO
}

// NewRouter создаёт роутер; реплик может не быть вовсе.
func NewRouter(primary Primary, replicas ...Replica) *Router {
	// TODO
	return &Router{}
}

// History читает историю покупателя. fromReplica — ответила реплика.
func (r *Router) History(ctx context.Context, customerID int64, token LSN) (orders []Order, fromReplica bool, err error) {
	// TODO
	return nil, false, nil
}
