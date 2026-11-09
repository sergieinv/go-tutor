package task

import (
	"context"
	"slices"
	"strconv"
	"sync/atomic"
	"testing"
	"time"
)

func customers(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = "customer-" + strconv.Itoa(i)
	}
	return out
}

func mustRouter(t *testing.T, shards ...string) *Router {
	t.Helper()
	r, err := NewRouter(shards...)
	if err != nil || r == nil {
		t.Fatalf("NewRouter(%q) = %v, %v", shards, r, err)
	}
	return r
}

func TestShardIsStable(t *testing.T) {
	r := mustRouter(t, "orders-1", "orders-2", "orders-3")
	for _, c := range customers(100) {
		s := r.Shard(c)
		if !slices.Contains([]string{"orders-1", "orders-2", "orders-3"}, s) {
			t.Fatalf("Shard(%q) = %q — такого шарда нет", c, s)
		}
		if again := r.Shard(c); again != s {
			t.Fatalf("Shard(%q): сначала %q, потом %q", c, s, again)
		}
	}
}

func TestAddMovesFewKeys(t *testing.T) {
	r := mustRouter(t, "s1", "s2", "s3", "s4")
	keys := customers(10_000)
	before := make(map[string]string, len(keys))
	for _, k := range keys {
		before[k] = r.Shard(k)
	}
	if err := r.Add("s5"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	moved := 0
	for _, k := range keys {
		if after := r.Shard(k); after != before[k] {
			moved++
			if after != "s5" {
				t.Fatalf("%s переехал с %s на %s, а не на новый шард", k, before[k], after)
			}
		}
	}
	// В среднем должна переехать 1/5 ключей; hash % N двигает 4/5.
	if moved == 0 || moved > 3000 {
		t.Fatalf("при добавлении пятого шарда переехало %d ключей из 10000, want 1..3000", moved)
	}
}

// fakeStore — шард в памяти: считает вызовы и запоминает limit.
type fakeStore struct {
	orders   []Order
	err      error
	wait     func(ctx context.Context) error // если задана — вызывается перед ответом
	calls    atomic.Int32
	gotLimit atomic.Int32
}

func (f *fakeStore) RecentOrders(ctx context.Context, limit int) ([]Order, error) {
	f.calls.Add(1)
	f.gotLimit.Store(int32(limit))
	if f.wait != nil {
		if err := f.wait(ctx); err != nil {
			return nil, err
		}
	}
	if f.err != nil {
		return nil, f.err
	}
	// Отдаём limit самых новых, но в обратном порядке: сортировать — забота вызывающего.
	sorted := slices.Clone(f.orders)
	slices.SortFunc(sorted, func(a, b Order) int { return b.CreatedAt.Compare(a.CreatedAt) })
	if len(sorted) > limit {
		sorted = sorted[:limit]
	}
	slices.Reverse(sorted)
	return sorted, nil
}

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

func order(id int64, minutes int) Order {
	return Order{ID: id, CustomerID: "c" + strconv.FormatInt(id, 10), CreatedAt: t0.Add(time.Duration(minutes) * time.Minute)}
}

func ids(orders []Order) []int64 {
	out := make([]int64, len(orders))
	for i, o := range orders {
		out[i] = o.ID
	}
	return out
}

func TestRecentOrdersMerges(t *testing.T) {
	stores := map[string]Store{
		"s1": &fakeStore{orders: []Order{order(1, 1), order(2, 5), order(3, 9)}},
		"s2": &fakeStore{orders: []Order{order(4, 2), order(5, 8)}},
		"s3": &fakeStore{orders: []Order{order(6, 7)}},
	}
	got, err := RecentOrders(context.Background(), stores, 4)
	if err != nil {
		t.Fatalf("RecentOrders: %v", err)
	}
	if want := []int64{3, 5, 6, 2}; !slices.Equal(ids(got), want) {
		t.Fatalf("RecentOrders(limit=4) = заказы %v, want %v (новые первыми)", ids(got), want)
	}
}
