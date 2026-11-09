package task

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"slices"
	"sync"
	"time"

	"golang.org/x/sync/errgroup"
)

var (
	ErrEmptyName      = errors.New("пустое имя шарда")
	ErrDuplicateShard = errors.New("шард уже есть")
	ErrUnknownShard   = errors.New("нет такого шарда")
)

// Router выбирает шард по покупателю: rendezvous hashing (HRW).
type Router struct {
	mu     sync.RWMutex
	shards []string // отсортированы
}

func NewRouter(shards ...string) (*Router, error) {
	r := &Router{}
	for _, s := range shards {
		if err := r.Add(s); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// hash64 — FNV-1a и финализатор fmix64: одинаков в любом процессе.
func hash64(parts ...string) uint64 {
	h := fnv.New64a()
	for i, p := range parts {
		if i > 0 {
			h.Write([]byte{0}) // разделитель: ("ab","c") ≠ ("a","bc")
		}
		h.Write([]byte(p))
	}
	x := h.Sum64()
	x ^= x >> 33
	x *= 0xff51afd7ed558ccd
	x ^= x >> 33
	x *= 0xc4ceb9fe1a85ec53
	x ^= x >> 33
	return x
}

// Shard возвращает шард с наибольшим весом hash(шард, ключ);
// при равенстве весов — с меньшим именем. Пустой роутер — "".
func (r *Router) Shard(customerID string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	best, owner := uint64(0), ""
	for _, s := range r.shards {
		if w := hash64(s, customerID); owner == "" || w > best {
			best, owner = w, s
		}
	}
	return owner
}

func (r *Router) Add(name string) error {
	if name == "" {
		return ErrEmptyName
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	i, found := slices.BinarySearch(r.shards, name)
	if found {
		return fmt.Errorf("%w: %s", ErrDuplicateShard, name)
	}
	r.shards = slices.Insert(r.shards, i, name)
	return nil
}

func (r *Router) Remove(name string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	i, found := slices.BinarySearch(r.shards, name)
	if !found {
		return fmt.Errorf("%w: %s", ErrUnknownShard, name)
	}
	r.shards = slices.Delete(r.shards, i, i+1)
	return nil
}

func (r *Router) Shards() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return slices.Clone(r.shards)
}

type Order struct {
	ID         int64
	CustomerID string
	CreatedAt  time.Time
}

type Store interface {
	RecentOrders(ctx context.Context, limit int) ([]Order, error)
}

// RecentOrders опрашивает все шарды параллельно (scatter), объединяет
// и сортирует ответы (gather) и возвращает limit самых новых заказов.
func RecentOrders(ctx context.Context, stores map[string]Store, limit int) ([]Order, error) {
	if limit <= 0 {
		return nil, nil
	}
	g, gctx := errgroup.WithContext(ctx)
	var mu sync.Mutex
	var all []Order
	for name, st := range stores {
		g.Go(func() error {
			// Каждый шард отдаёт свои limit самых новых: меньше нельзя —
			// все limit лучших могут оказаться на одном шарде.
			orders, err := st.RecentOrders(gctx, limit)
			if err != nil {
				return fmt.Errorf("шард %s: %w", name, err)
			}
			mu.Lock()
			all = append(all, orders...)
			mu.Unlock()
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	slices.SortFunc(all, func(a, b Order) int {
		if c := b.CreatedAt.Compare(a.CreatedAt); c != 0 {
			return c
		}
		return cmp.Compare(a.ID, b.ID)
	})
	if len(all) > limit {
		all = all[:limit]
	}
	return all, nil
}
