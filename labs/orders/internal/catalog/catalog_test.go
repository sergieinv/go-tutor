package catalog_test

import (
	"context"
	"log/slog"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/prometheus/client_golang/prometheus"

	"gotutor/labs/orders/internal/catalog"
	"gotutor/labs/orders/internal/orders"
)

// source — каталог «в базе»: считает запросы и может их задерживать.
type source struct {
	mu       sync.Mutex
	products map[int64]orders.Product
	calls    atomic.Int64
	gate     chan struct{} // не nil — запрос ждёт, пока канал не закроют
}

func newSource() *source {
	return &source{products: map[int64]orders.Product{
		1: {ID: 1, Name: "Клавиатура", PriceCents: 499000},
		2: {ID: 2, Name: "Мышь", PriceCents: 199000},
	}}
}

func (s *source) ProductsByIDs(ctx context.Context, ids []int64) ([]orders.Product, error) {
	s.calls.Add(1)
	if s.gate != nil {
		select {
		case <-s.gate:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []orders.Product
	for _, id := range ids {
		if p, ok := s.products[id]; ok {
			out = append(out, p)
		}
	}
	slices.SortFunc(out, func(a, b orders.Product) int { return int(a.ID - b.ID) })
	return out, nil
}

func (s *source) setPrice(id, price int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	p := s.products[id]
	p.PriceCents = price
	s.products[id] = p
}

type harness struct {
	mr    *miniredis.Miniredis
	src   *source
	reg   *prometheus.Registry
	cache *catalog.Cache
}

func newHarness(t *testing.T, cfg catalog.Config) *harness {
	t.Helper()
	mr := miniredis.RunT(t)
	kv, err := catalog.NewValkey(mr.Addr(), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(kv.Close)
	h := &harness{mr: mr, src: newSource(), reg: prometheus.NewRegistry()}
	cfg.Registerer = h.reg
	if cfg.Rand == nil {
		cfg.Rand = func() float64 { return 0.5 } // джиттер ровно 0
	}
	h.cache = catalog.New(kv, h.src, cfg)
	return h
}

func (h *harness) lookups(result string) float64 {
	return counter(h.reg, "orders_catalog_cache_lookups_total", result)
}

// counter — значение счётчика name из реестра; result — значение метки
// result ("" — у счётчика нет меток).
func counter(reg *prometheus.Registry, name, result string) float64 {
	mfs, _ := reg.Gather()
	var sum float64
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			match := result == ""
			for _, l := range m.GetLabel() {
				if l.GetName() == "result" && l.GetValue() == result {
					match = true
				}
			}
			if match {
				sum += m.GetCounter().GetValue()
			}
		}
	}
	return sum
}

func get(t *testing.T, c orders.Catalog, ids ...int64) []orders.Product {
	t.Helper()
	ps, err := c.ProductsByIDs(t.Context(), ids)
	if err != nil {
		t.Fatalf("ProductsByIDs(%v): %v", ids, err)
	}
	return ps
}

func TestCacheAside(t *testing.T) {
	h := newHarness(t, catalog.Config{TTL: time.Minute})

	first := get(t, h.cache, 2, 1, 2)
	if len(first) != 2 || first[0].ID != 1 || first[1].ID != 2 {
		t.Fatalf("products = %+v, want 1 and 2 sorted, no duplicates", first)
	}
	if n := h.src.calls.Load(); n != 2 {
		t.Errorf("db calls after cold read = %d, want 2 (one per missing key)", n)
	}
	if ttl := h.mr.TTL("orders:catalog:v1:1"); ttl != time.Minute {
		t.Errorf("TTL = %v, want 1m (jitter 0 with Rand = 0.5)", ttl)
	}

	second := get(t, h.cache, 1, 2)
	if !slices.Equal(first, second) {
		t.Errorf("cached = %+v, want %+v", second, first)
	}
	if n := h.src.calls.Load(); n != 2 {
		t.Errorf("db calls after warm read = %d, want still 2", n)
	}
	if hit, miss := h.lookups("hit"), h.lookups("miss"); hit != 2 || miss != 2 {
		t.Errorf("hit/miss = %v/%v, want 2/2", hit, miss)
	}

	// Цена в базе поменялась: кеш отдаёт старую до истечения TTL.
	h.src.setPrice(1, 1)
	if p := get(t, h.cache, 1); p[0].PriceCents != 499000 {
		t.Errorf("price before TTL = %d, want stale 499000", p[0].PriceCents)
	}
	h.mr.FastForward(time.Minute + time.Second)
	if p := get(t, h.cache, 1); p[0].PriceCents != 1 {
		t.Errorf("price after TTL = %d, want fresh 1", p[0].PriceCents)
	}
}

func TestTTLJitter(t *testing.T) {
	for _, tc := range []struct {
		rand float64
		want time.Duration
	}{
		{0, 90 * time.Second},         // −10%
		{0.999999, 110 * time.Second}, // почти +10%
	} {
		h := newHarness(t, catalog.Config{TTL: 100 * time.Second, Rand: func() float64 { return tc.rand }})
		get(t, h.cache, 1)
		if got := h.mr.TTL("orders:catalog:v1:1"); got.Round(time.Second) != tc.want {
			t.Errorf("rand %v: TTL = %v, want %v", tc.rand, got, tc.want)
		}
	}
}

func TestNegativeCaching(t *testing.T) {
	h := newHarness(t, catalog.Config{NegativeTTL: 10 * time.Second})

	for range 3 {
		if ps := get(t, h.cache, 404); len(ps) != 0 {
			t.Fatalf("products = %+v, want none", ps)
		}
	}
	if n := h.src.calls.Load(); n != 1 {
		t.Errorf("db calls = %d, want 1: «нет такого» закеширован", n)
	}
	if ttl := h.mr.TTL("orders:catalog:v1:404"); ttl != 10*time.Second {
		t.Errorf("negative TTL = %v, want 10s", ttl)
	}
	if n := h.lookups("negative_hit"); n != 2 {
		t.Errorf("negative hits = %v, want 2", n)
	}

	// Товар появился: после короткого TTL он виден.
	h.src.mu.Lock()
	h.src.products[404] = orders.Product{ID: 404, Name: "Новинка", PriceCents: 100}
	h.src.mu.Unlock()
	h.mr.FastForward(11 * time.Second)
	if ps := get(t, h.cache, 404); len(ps) != 1 {
		t.Errorf("after negative TTL: %+v, want the new product", ps)
	}
}

func TestDegradesToDBWhenValkeyIsDown(t *testing.T) {
	h := newHarness(t, catalog.Config{OpTimeout: time.Second})
	h.mr.Close()

	for range 2 {
		if ps := get(t, h.cache, 1, 2); len(ps) != 2 {
			t.Fatalf("products = %+v, want both from db", ps)
		}
	}
	if n := h.src.calls.Load(); n != 4 {
		t.Errorf("db calls = %d, want 4: каждый запрос — в базу", n)
	}
	if n := h.lookups("error"); n < 4 {
		t.Errorf("error lookups = %v, want ≥ 4", n)
	}

	// Valkey вернулся на том же адресе — кеш снова работает.
	if err := h.mr.Restart(); err != nil {
		t.Fatal(err)
	}
	// Первая команда после рестарта может упасть на старом соединении;
	// клиент переподключается при следующей. Несколько попыток, без пауз.
	recovered := false
	for range 5 {
		get(t, h.cache, 1)
		before := h.src.calls.Load()
		get(t, h.cache, 1)
		if h.src.calls.Load() == before {
			recovered = true
			break
		}
	}
	if !recovered {
		t.Errorf("after restart: still reading from db")
	}
}

func TestDisabledBypassesValkey(t *testing.T) {
	var on atomic.Bool
	h := newHarness(t, catalog.Config{Enabled: on.Load})

	get(t, h.cache, 1)
	get(t, h.cache, 1)
	if n := h.src.calls.Load(); n != 2 {
		t.Errorf("db calls = %d, want 2", n)
	}
	if keys := h.mr.Keys(); len(keys) != 0 {
		t.Errorf("valkey keys = %v, want none: выключенный кеш не пишет", keys)
	}
	if n := h.lookups("bypass"); n != 2 {
		t.Errorf("bypass = %v, want 2", n)
	}
	on.Store(true)
	get(t, h.cache, 1)
	get(t, h.cache, 1)
	if n := h.src.calls.Load(); n != 3 {
		t.Errorf("db calls after enabling = %d, want 3", n)
	}
}

// memKV — KV в памяти для теста stampede: synctest не дружит с сетью
// miniredis, а здесь важна только конкуренция за один ключ.
type memKV struct {
	mu sync.Mutex
	m  map[string]string
}

func (k *memKV) MGet(_ context.Context, keys []string) ([]*string, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	out := make([]*string, len(keys))
	for i, key := range keys {
		if v, ok := k.m[key]; ok {
			out[i] = &v
		}
	}
	return out, nil
}

func (k *memKV) Set(_ context.Context, key, value string, _ time.Duration) error {
	k.mu.Lock()
	defer k.mu.Unlock()
	k.m[key] = value
	return nil
}

// TestStampede: 50 одновременных промахов по одному ключу — один запрос в
// базу. synctest.Wait дожидается, пока все горутины встанут в очередь
// singleflight (заблокируются), — без пауз и порогов по времени.
func TestStampede(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newSource()
		src.gate = make(chan struct{})
		reg := prometheus.NewRegistry()
		cache := catalog.New(&memKV{m: map[string]string{}}, src, catalog.Config{Registerer: reg})

		const n = 50
		var wg sync.WaitGroup
		errs := make(chan error, n)
		for range n {
			wg.Go(func() {
				ps, err := cache.ProductsByIDs(context.Background(), []int64{1})
				if err == nil && len(ps) != 1 {
					err = context.DeadlineExceeded // любой ненулевой признак ошибки
				}
				errs <- err
			})
		}
		synctest.Wait() // все 50 ждут: один — базу, остальные — его
		if c := src.calls.Load(); c != 1 {
			t.Fatalf("db calls while loading = %d, want 1", c)
		}
		close(src.gate)
		wg.Wait()
		close(errs)
		for err := range errs {
			if err != nil {
				t.Fatalf("ProductsByIDs: %v", err)
			}
		}
		if c := src.calls.Load(); c != 1 {
			t.Errorf("db calls = %d, want 1", c)
		}
		if got := counter(reg, "orders_catalog_cache_coalesced_total", ""); got != n-1 {
			t.Errorf("coalesced = %v, want %d", got, n-1)
		}
	})
}

// TestLeaderCancelDoesNotFailFollowers: клиент, начавший загрузку, ушёл —
// остальные, ждущие тот же ключ, получают результат.
func TestLeaderCancelDoesNotFailFollowers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		src := newSource()
		src.gate = make(chan struct{})
		cache := catalog.New(&memKV{m: map[string]string{}}, src, catalog.Config{})

		leaderCtx, cancel := context.WithCancel(context.Background())
		leaderErr := make(chan error, 1)
		go func() {
			_, err := cache.ProductsByIDs(leaderCtx, []int64{1})
			leaderErr <- err
		}()
		synctest.Wait()
		follower := make(chan []orders.Product, 1)
		go func() {
			ps, _ := cache.ProductsByIDs(context.Background(), []int64{1})
			follower <- ps
		}()
		synctest.Wait()
		cancel()
		if err := <-leaderErr; err == nil {
			t.Errorf("leader: want context error")
		}
		close(src.gate)
		if ps := <-follower; len(ps) != 1 {
			t.Errorf("follower got %+v, want the product", ps)
		}
	})
}
