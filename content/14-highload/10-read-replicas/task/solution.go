package task

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
)

// ErrBadLSN — строка не похожа на LSN PostgreSQL.
var ErrBadLSN = errors.New("bad lsn")

// LSN — позиция в журнале WAL: старшие 32 бита до «/», младшие — после.
type LSN uint64

// ParseLSN разбирает LSN в текстовом виде PostgreSQL: "16/B374D848".
func ParseLSN(s string) (LSN, error) {
	hi, lo, ok := strings.Cut(s, "/")
	if !ok || !isHex(hi) || !isHex(lo) {
		return 0, fmt.Errorf("%w: %q", ErrBadLSN, s)
	}
	h, _ := strconv.ParseUint(hi, 16, 32)
	l, _ := strconv.ParseUint(lo, 16, 32)
	return LSN(h<<32 | l), nil
}

// isHex — от 1 до 8 шестнадцатеричных цифр и ничего больше.
func isHex(s string) bool {
	if len(s) < 1 || len(s) > 8 {
		return false
	}
	for _, c := range s {
		switch {
		case c >= '0' && c <= '9', c >= 'a' && c <= 'f', c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}

// String печатает LSN так же, как PostgreSQL.
func (l LSN) String() string {
	return fmt.Sprintf("%X/%X", uint64(l)>>32, uint64(l)&0xFFFFFFFF)
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
	primary  Primary
	replicas []Replica
	next     atomic.Uint64
}

// NewRouter создаёт роутер; реплик может не быть вовсе.
func NewRouter(primary Primary, replicas ...Replica) *Router {
	return &Router{primary: primary, replicas: replicas}
}

// History читает историю покупателя. fromReplica — ответила реплика.
func (r *Router) History(ctx context.Context, customerID int64, token LSN) (orders []Order, fromReplica bool, err error) {
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	n := len(r.replicas)
	if n > 0 {
		// Каждый вызов начинает обход со следующей реплики.
		start := int((r.next.Add(1) - 1) % uint64(n))
		for i := range n {
			rep := r.replicas[(start+i)%n]
			if token > 0 {
				lsn, err := rep.ReplayLSN(ctx)
				if ctx.Err() != nil {
					return nil, false, ctx.Err()
				}
				if err != nil || lsn < token {
					continue // отстаёт или недоступна — следующая
				}
			}
			orders, err := rep.History(ctx, customerID)
			if ctx.Err() != nil {
				return nil, false, ctx.Err()
			}
			if err == nil {
				return orders, true, nil
			}
		}
	}
	orders, err = r.primary.History(ctx, customerID)
	return orders, false, err
}
