package task

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func shardNames(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("orders-%02d", i)
	}
	return out
}

func TestHiddenUniformity(t *testing.T) {
	keys := customers(10_000)
	for n := 1; n <= 10; n++ {
		r := mustRouter(t, shardNames(n)...)
		count := map[string]int{}
		for _, k := range keys {
			count[r.Shard(k)]++
		}
		avg := float64(len(keys)) / float64(n)
		for _, s := range shardNames(n) {
			if c := float64(count[s]); c < 0.6*avg || c > 1.4*avg {
				t.Errorf("N=%d: у шарда %s %v ключей из 10000, want от %.0f до %.0f", n, s, c, 0.6*avg, 1.4*avg)
			}
		}
		if len(count) != n {
			t.Errorf("N=%d: ключи попали в шарды %v", n, count)
		}
	}
}

func TestHiddenAddMovesOnlyToNew(t *testing.T) {
	keys := customers(10_000)
	for n := 1; n <= 8; n++ {
		names := shardNames(n + 1)
		r := mustRouter(t, names[:n]...)
		before := make(map[string]string, len(keys))
		for _, k := range keys {
			before[k] = r.Shard(k)
		}
		if err := r.Add(names[n]); err != nil {
			t.Fatalf("Add: %v", err)
		}
		moved := 0
		for _, k := range keys {
			after := r.Shard(k)
			if after == before[k] {
				continue
			}
			moved++
			if after != names[n] {
				t.Fatalf("N=%d→%d: %s переехал с %s на %s, а не на новый шард", n, n+1, k, before[k], after)
			}
		}
		limit := 1.5 * float64(len(keys)) / float64(n+1)
		if moved == 0 || float64(moved) > limit {
			t.Errorf("N=%d→%d: переехало %d ключей, want 1..%.0f", n, n+1, moved, limit)
		}
	}
}

func TestHiddenRemoveMovesOnlyItsKeys(t *testing.T) {
	keys := customers(5_000)
	r := mustRouter(t, shardNames(5)...)
	before := make(map[string]string, len(keys))
	for _, k := range keys {
		before[k] = r.Shard(k)
	}
	if err := r.Remove("orders-02"); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	for _, k := range keys {
		after := r.Shard(k)
		if after == "orders-02" {
			t.Fatalf("%s всё ещё на удалённом шарде", k)
		}
		if before[k] != "orders-02" && after != before[k] {
			t.Fatalf("%s переехал с %s на %s, хотя удаляли orders-02", k, before[k], after)
		}
	}
	// Вернули шард — распределение то же, что было.
	if err := r.Add("orders-02"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	for _, k := range keys {
		if after := r.Shard(k); after != before[k] {
			t.Fatalf("после Remove+Add %s на %s, а был на %s", k, after, before[k])
		}
	}
}

func TestHiddenOrderIndependent(t *testing.T) {
	a := mustRouter(t, "c", "a", "b", "d")
	b := mustRouter(t)
	for _, s := range []string{"d", "b", "a", "c"} {
		if err := b.Add(s); err != nil {
			t.Fatalf("Add(%q): %v", s, err)
		}
	}
	for _, k := range customers(2_000) {
		if a.Shard(k) != b.Shard(k) {
			t.Fatalf("Shard(%q): %q у одного роутера и %q у другого — результат зависит от порядка шардов", k, a.Shard(k), b.Shard(k))
		}
	}
}

func TestHiddenErrors(t *testing.T) {
	if r, err := NewRouter("a", "b", "a"); !errors.Is(err, ErrDuplicateShard) || r != nil {
		t.Errorf(`NewRouter("a","b","a") = %v, %v; want nil, ErrDuplicateShard`, r, err)
	}
	if r, err := NewRouter("a", ""); !errors.Is(err, ErrEmptyName) || r != nil {
		t.Errorf(`NewRouter("a","") = %v, %v; want nil, ErrEmptyName`, r, err)
	}
	r := mustRouter(t, "a")
	if err := r.Add("a"); !errors.Is(err, ErrDuplicateShard) {
		t.Errorf(`Add("a") повторно: %v, want ErrDuplicateShard`, err)
	}
	if err := r.Add(""); !errors.Is(err, ErrEmptyName) {
		t.Errorf(`Add(""): %v, want ErrEmptyName`, err)
	}
	if err := r.Remove("zzz"); !errors.Is(err, ErrUnknownShard) {
		t.Errorf(`Remove("zzz"): %v, want ErrUnknownShard`, err)
	}
	if got := r.Shards(); !slices.Equal(got, []string{"a"}) {
		t.Errorf("после ошибок Shards() = %q, want [a]", got)
	}
}

func TestHiddenEmptyRouter(t *testing.T) {
	r := mustRouter(t)
	if s := r.Shard("customer-1"); s != "" {
		t.Errorf(`пустой роутер: Shard = %q, want ""`, s)
	}
	if err := r.Add("only"); err != nil {
		t.Fatal(err)
	}
	for _, k := range customers(100) {
		if s := r.Shard(k); s != "only" {
			t.Fatalf("один шард: Shard(%q) = %q", k, s)
		}
	}
	if err := r.Remove("only"); err != nil {
		t.Fatal(err)
	}
	if s := r.Shard("customer-1"); s != "" {
		t.Errorf(`после удаления последнего шарда Shard = %q, want ""`, s)
	}
	if got := r.Shards(); len(got) != 0 {
		t.Errorf("Shards() = %q, want пусто", got)
	}
}

func TestHiddenShardsSortedCopy(t *testing.T) {
	r := mustRouter(t, "b", "c", "a")
	got := r.Shards()
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("Shards() = %q, want [a b c]", got)
	}
	got[0] = "zzz"
	if again := r.Shards(); !slices.Equal(again, []string{"a", "b", "c"}) {
		t.Fatalf("изменение результата Shards() поменяло роутер: %q", again)
	}
}

func TestHiddenConcurrentUse(t *testing.T) {
	r := mustRouter(t, shardNames(4)...)
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i, k := range customers(500) {
				if s := r.Shard(k); s == "" {
					t.Errorf("Shard(%q) = \"\" при непустом роутере", k)
					return
				}
				if g == 0 && i%50 == 0 {
					name := fmt.Sprintf("extra-%d", i)
					_ = r.Add(name)
					_ = r.Shards()
					_ = r.Remove(name)
				}
			}
		})
	}
	wg.Wait()
}

// --- scatter-gather ---

// withTimeout не даёт тесту зависнуть, если RecentOrders не возвращается.
func withTimeout(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RecentOrders не вернулся за 5 с")
	}
}

func TestHiddenRecentOrdersParallel(t *testing.T) {
	const n = 4
	var arrived atomic.Int32
	all := make(chan struct{})
	stores := map[string]Store{}
	for i := range n {
		stores[fmt.Sprintf("s%d", i)] = &fakeStore{
			orders: []Order{order(int64(i+1), i)},
			// Каждый шард ждёт, пока запрос придёт ко всем: при последовательном
			// опросе второй шард так и не будет вызван, первый сдастся по таймеру.
			wait: func(ctx context.Context) error {
				if arrived.Add(1) == n {
					close(all)
				}
				select {
				case <-all:
					return nil
				case <-ctx.Done():
					return ctx.Err()
				case <-time.After(3 * time.Second):
					return errors.New("шарды опрашиваются последовательно")
				}
			},
		}
	}
	withTimeout(t, func() {
		got, err := RecentOrders(context.Background(), stores, 10)
		if err != nil {
			t.Errorf("RecentOrders: %v", err)
			return
		}
		if want := []int64{4, 3, 2, 1}; !slices.Equal(ids(got), want) {
			t.Errorf("RecentOrders = %v, want %v", ids(got), want)
		}
	})
}

func TestHiddenRecentOrdersLimit(t *testing.T) {
	// Все лучшие заказы — на одном шарде: каждый шард обязан отдать limit.
	s1 := &fakeStore{orders: []Order{order(1, 50), order(2, 40), order(3, 30), order(4, 20)}}
	s2 := &fakeStore{orders: []Order{order(5, 1), order(6, 2)}}
	stores := map[string]Store{"s1": s1, "s2": s2}
	got, err := RecentOrders(context.Background(), stores, 3)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{1, 2, 3}; !slices.Equal(ids(got), want) {
		t.Errorf("RecentOrders(limit=3) = %v, want %v", ids(got), want)
	}
	for name, s := range map[string]*fakeStore{"s1": s1, "s2": s2} {
		if c, l := s.calls.Load(), s.gotLimit.Load(); c != 1 || l != 3 {
			t.Errorf("шард %s: вызовов %d с limit %d, want 1 вызов с limit 3", name, c, l)
		}
	}

	// limit больше, чем заказов всего, — все заказы.
	got, err = RecentOrders(context.Background(), stores, 100)
	if err != nil || len(got) != 6 {
		t.Errorf("RecentOrders(limit=100): %d заказов, %v; want 6, nil", len(got), err)
	}
}

func TestHiddenRecentOrdersNonPositiveLimit(t *testing.T) {
	s := &fakeStore{orders: []Order{order(1, 1)}}
	for _, limit := range []int{0, -5} {
		got, err := RecentOrders(context.Background(), map[string]Store{"s": s}, limit)
		if err != nil || len(got) != 0 {
			t.Errorf("RecentOrders(limit=%d) = %v, %v; want пусто, nil", limit, ids(got), err)
		}
	}
	if c := s.calls.Load(); c != 0 {
		t.Errorf("при limit <= 0 шард вызван %d раз, want 0", c)
	}
}

func TestHiddenRecentOrdersNoStores(t *testing.T) {
	got, err := RecentOrders(context.Background(), map[string]Store{}, 5)
	if err != nil || len(got) != 0 {
		t.Errorf("RecentOrders без шардов = %v, %v; want пусто, nil", got, err)
	}
}

// Шард вернул больше, чем просили, — в ответе всё равно не больше limit.
type greedyStore struct{ orders []Order }

func (g greedyStore) RecentOrders(context.Context, int) ([]Order, error) { return g.orders, nil }

func TestHiddenRecentOrdersGreedyShard(t *testing.T) {
	stores := map[string]Store{
		"a": greedyStore{[]Order{order(1, 1), order(2, 2), order(3, 3), order(4, 4)}},
		"b": greedyStore{[]Order{order(5, 5)}},
	}
	got, err := RecentOrders(context.Background(), stores, 2)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{5, 4}; !slices.Equal(ids(got), want) {
		t.Errorf("RecentOrders(limit=2) = %v, want %v", ids(got), want)
	}
}

func TestHiddenRecentOrdersTieBreak(t *testing.T) {
	stores := map[string]Store{
		"a": &fakeStore{orders: []Order{order(30, 5), order(10, 5)}},
		"b": &fakeStore{orders: []Order{order(20, 5), order(40, 1)}},
	}
	got, err := RecentOrders(context.Background(), stores, 4)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{10, 20, 30, 40}; !slices.Equal(ids(got), want) {
		t.Errorf("RecentOrders = %v, want %v (одинаковое время — по возрастанию ID)", ids(got), want)
	}
}

var errShardDown = errors.New("connection refused")

func TestHiddenRecentOrdersErrorCancelsOthers(t *testing.T) {
	var canceled atomic.Int32
	slow := func(ctx context.Context) error {
		select {
		case <-ctx.Done():
			canceled.Add(1)
			return ctx.Err()
		case <-time.After(3 * time.Second):
			return nil
		}
	}
	stores := map[string]Store{
		"orders-eu": &fakeStore{err: errShardDown},
		"orders-us": &fakeStore{wait: slow, orders: []Order{order(1, 1)}},
		"orders-as": &fakeStore{wait: slow, orders: []Order{order(2, 2)}},
	}
	withTimeout(t, func() {
		got, err := RecentOrders(context.Background(), stores, 5)
		if !errors.Is(err, errShardDown) {
			t.Errorf("err = %v, want обёртку над ошибкой шарда (errors.Is)", err)
			return
		}
		if !strings.Contains(err.Error(), "orders-eu") {
			t.Errorf("в ошибке %q нет имени шарда orders-eu", err)
		}
		if got != nil {
			t.Errorf("при ошибке вернулись заказы %v, want nil", ids(got))
		}
		// RecentOrders возвращается, только когда все запросы к шардам завершились.
		if c := canceled.Load(); c != 2 {
			t.Errorf("отменено запросов к остальным шардам: %d, want 2 (контекст должен отменяться после первой ошибки)", c)
		}
	})
}

func TestHiddenRecentOrdersParentContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	honest := func(ctx context.Context) error { return ctx.Err() }
	stores := map[string]Store{
		"a": &fakeStore{wait: honest},
		"b": &fakeStore{wait: honest},
	}
	withTimeout(t, func() {
		_, err := RecentOrders(ctx, stores, 5)
		if !errors.Is(err, context.Canceled) {
			t.Errorf("с отменённым контекстом err = %v, want context.Canceled", err)
		}
	})
}
