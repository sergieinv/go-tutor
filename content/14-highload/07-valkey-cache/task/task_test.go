package task

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"
)

type setCall struct {
	key, val string
	ttl      time.Duration
}

// fakeCache — кеш в памяти, который записывает все вызовы.
type fakeCache struct {
	mu     sync.Mutex
	data   map[string]string
	sets   []setCall
	dels   []string
	gets   int
	getErr error // не nil — Get отвечает этой ошибкой
	delErr error
	ctxs   []context.Context
}

func newFakeCache() *fakeCache { return &fakeCache{data: map[string]string{}} }

func (f *fakeCache) Get(ctx context.Context, key string) (string, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.gets++
	f.ctxs = append(f.ctxs, ctx)
	if f.getErr != nil {
		return "", false, f.getErr
	}
	v, ok := f.data[key]
	return v, ok, nil
}

func (f *fakeCache) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctxs = append(f.ctxs, ctx)
	f.sets = append(f.sets, setCall{key, val, ttl})
	f.data[key] = val
	return nil
}

func (f *fakeCache) Del(ctx context.Context, key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ctxs = append(f.ctxs, ctx)
	f.dels = append(f.dels, key)
	if f.delErr != nil {
		return f.delErr
	}
	delete(f.data, key)
	return nil
}

// fakeStore — база со счётчиком чтений.
type fakeStore struct {
	mu      sync.Mutex
	rows    map[int64]Product
	reads   int
	readErr error
	saveErr error
	ctxs    []context.Context
}

func newFakeStore(ps ...Product) *fakeStore {
	s := &fakeStore{rows: map[int64]Product{}}
	for _, p := range ps {
		s.rows[p.ID] = p
	}
	return s
}

func (s *fakeStore) Product(ctx context.Context, id int64) (Product, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reads++
	s.ctxs = append(s.ctxs, ctx)
	if s.readErr != nil {
		return Product{}, s.readErr
	}
	p, ok := s.rows[id]
	if !ok {
		return Product{}, ErrNotFound
	}
	return p, nil
}

func (s *fakeStore) SaveProduct(ctx context.Context, p Product) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctxs = append(s.ctxs, ctx)
	if s.saveErr != nil {
		return s.saveErr
	}
	s.rows[p.ID] = p
	return nil
}

var apple = Product{ID: 1, Name: "яблоки", Price: 120}

func decode(t *testing.T, val string) Product {
	t.Helper()
	var p Product
	if err := json.Unmarshal([]byte(val), &p); err != nil {
		t.Fatalf("в кеше не JSON товара: %q (%v)", val, err)
	}
	return p
}

func TestCacheAside(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple)
	cat := NewCatalog(cache, store, Config{Prefix: "shop:", TTL: time.Minute, NegativeTTL: 10 * time.Second})
	for i := range 3 {
		p, err := cat.Product(context.Background(), 1)
		if err != nil || p != apple {
			t.Fatalf("вызов %d: Product = %+v, %v; want %+v, nil", i+1, p, err, apple)
		}
	}
	if store.reads != 1 {
		t.Errorf("чтений базы %d, want 1: второй и третий вызов — из кеша", store.reads)
	}
	if len(cache.sets) != 1 || cache.sets[0].key != "shop:product:1" || cache.sets[0].ttl != time.Minute {
		t.Fatalf("Set = %+v, want один вызов с ключом shop:product:1 и TTL 1m", cache.sets)
	}
	if got := decode(t, cache.sets[0].val); got != apple {
		t.Errorf("в кеше %+v, want %+v", got, apple)
	}
}

func TestNegativeCaching(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple)
	cat := NewCatalog(cache, store, Config{TTL: time.Minute, NegativeTTL: 10 * time.Second})
	for i := range 2 {
		if _, err := cat.Product(context.Background(), 404); err != ErrNotFound {
			t.Fatalf("вызов %d: err = %v, want ErrNotFound", i+1, err)
		}
	}
	if store.reads != 1 {
		t.Errorf("чтений базы %d, want 1: «товара нет» тоже кешируется", store.reads)
	}
	want := setCall{"product:404", "", 10 * time.Second}
	if len(cache.sets) != 1 || cache.sets[0] != want {
		t.Errorf("Set = %+v, want [%+v]", cache.sets, want)
	}
}

func TestSaveInvalidates(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple)
	cat := NewCatalog(cache, store, Config{TTL: time.Minute, NegativeTTL: time.Second})
	cat.Product(context.Background(), 1)
	cheaper := Product{ID: 1, Name: "яблоки", Price: 99}
	if err := cat.Save(context.Background(), cheaper); err != nil {
		t.Fatalf("Save = %v", err)
	}
	if len(cache.dels) != 1 || cache.dels[0] != "product:1" {
		t.Fatalf("Del = %v, want [product:1]", cache.dels)
	}
	p, err := cat.Product(context.Background(), 1)
	if err != nil || p != cheaper {
		t.Errorf("после Save Product = %+v, %v; want %+v", p, err, cheaper)
	}
}
