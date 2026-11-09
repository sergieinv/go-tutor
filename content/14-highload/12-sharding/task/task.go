package task

import (
	"context"
	"errors"
	"time"
)

var (
	ErrEmptyName      = errors.New("пустое имя шарда")
	ErrDuplicateShard = errors.New("шард уже есть")
	ErrUnknownShard   = errors.New("нет такого шарда")
)

// Router выбирает шард «Заказов» по покупателю. Алгоритм — consistent
// hashing с виртуальными узлами или rendezvous hashing, на ваш выбор.
// Router безопасен для одновременного использования.
type Router struct {
	// TODO: поля роутера.
}

// NewRouter создаёт роутер с заданными шардами. Пустое имя —
// ErrEmptyName, повтор имени — ErrDuplicateShard (роутер тогда nil).
func NewRouter(shards ...string) (*Router, error) {
	// TODO
	return &Router{}, nil
}

// Shard возвращает имя шарда покупателя; если шардов нет — "".
func (r *Router) Shard(customerID string) string {
	// TODO
	return ""
}

// Add добавляет шард: пустое имя — ErrEmptyName, уже есть — ErrDuplicateShard.
func (r *Router) Add(name string) error {
	// TODO
	return nil
}

// Remove убирает шард; если его нет — ErrUnknownShard.
func (r *Router) Remove(name string) error {
	// TODO
	return nil
}

// Shards возвращает имена шардов по возрастанию (копию).
func (r *Router) Shards() []string {
	// TODO
	return nil
}

type Order struct {
	ID         int64
	CustomerID string
	CreatedAt  time.Time
}

// Store — один шард «Заказов». RecentOrders возвращает не больше limit
// самых новых заказов шарда (порядок не гарантирован).
type Store interface {
	RecentOrders(ctx context.Context, limit int) ([]Order, error)
}

// RecentOrders — запрос «последние заказы» по всем шардам: опросить
// все шарды параллельно, объединить, отсортировать и вернуть limit
// самых новых. Подробности — в условии.
func RecentOrders(ctx context.Context, stores map[string]Store, limit int) ([]Order, error) {
	// TODO
	return nil, nil
}
