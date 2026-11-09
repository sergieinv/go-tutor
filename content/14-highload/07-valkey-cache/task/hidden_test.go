package task

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/valkey-io/valkey-go"
)

func TestHiddenKey(t *testing.T) {
	cat := NewCatalog(newFakeCache(), newFakeStore(), Config{Prefix: "run_42:", TTL: time.Minute})
	if k := cat.Key(42); k != "run_42:product:42" {
		t.Errorf("Key(42) = %q, want %q", k, "run_42:product:42")
	}
	if k := cat.Key(-7); k != "run_42:product:-7" {
		t.Errorf("Key(-7) = %q, want %q", k, "run_42:product:-7")
	}
}

// Jitter вызывается по разу на каждую запись о товаре и только для неё.
func TestHiddenJitter(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple, Product{ID: 2, Name: "груши", Price: 150})
	calls := 0
	jitter := []time.Duration{3 * time.Second, 7 * time.Second}
	cat := NewCatalog(cache, store, Config{TTL: time.Minute, NegativeTTL: 5 * time.Second, Jitter: func() time.Duration {
		d := jitter[calls%2]
		calls++
		return d
	}})
	ctx := context.Background()
	cat.Product(ctx, 1)
	cat.Product(ctx, 1) // попадание — без Jitter
	cat.Product(ctx, 404)
	cat.Product(ctx, 2)
	if calls != 2 {
		t.Fatalf("Jitter вызван %d раз, want 2 (по разу на запись о товаре)", calls)
	}
	if len(cache.sets) != 3 {
		t.Fatalf("Set = %+v, want 3 вызова", cache.sets)
	}
	want := []setCall{
		{"product:1", cache.sets[0].val, 63 * time.Second},
		{"product:404", "", 5 * time.Second},
		{"product:2", cache.sets[2].val, 67 * time.Second},
	}
	for i := range want {
		if cache.sets[i] != want[i] {
			t.Errorf("Set №%d = %+v, want %+v", i+1, cache.sets[i], want[i])
		}
	}
}

func TestHiddenNegativeDisabled(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore()
	cat := NewCatalog(cache, store, Config{TTL: time.Minute})
	for range 3 {
		if _, err := cat.Product(context.Background(), 5); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	}
	if store.reads != 3 || len(cache.sets) != 0 {
		t.Errorf("NegativeTTL 0: чтений базы %d, Set %+v; want 3 и ни одного Set", store.reads, cache.sets)
	}
}

// Ошибка базы (не ErrNotFound) возвращается и не кешируется.
func TestHiddenStoreErrorNotCached(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple)
	dbDown := errors.New("connection refused")
	store.readErr = dbDown
	cat := NewCatalog(cache, store, Config{TTL: time.Minute, NegativeTTL: time.Minute})
	for range 2 {
		if _, err := cat.Product(context.Background(), 1); !errors.Is(err, dbDown) {
			t.Fatalf("err = %v, want ошибку базы", err)
		}
	}
	if store.reads != 2 || len(cache.sets) != 0 {
		t.Fatalf("чтений базы %d, Set %+v; want 2 и ни одного Set", store.reads, cache.sets)
	}
	store.readErr = nil
	if p, err := cat.Product(context.Background(), 1); err != nil || p != apple {
		t.Errorf("база поднялась: Product = %+v, %v", p, err)
	}
}

// Кеш недоступен: ответ — из базы, кеш больше не трогаем.
func TestHiddenCacheDown(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple)
	cache.getErr = errors.New("valkey: connection refused")
	cat := NewCatalog(cache, store, Config{TTL: time.Minute, NegativeTTL: time.Minute})
	p, err := cat.Product(context.Background(), 1)
	if err != nil || p != apple {
		t.Fatalf("Product = %+v, %v; want %+v из базы", p, err, apple)
	}
	if _, err := cat.Product(context.Background(), 404); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound из базы", err)
	}
	if store.reads != 2 || len(cache.sets) != 0 || len(cache.dels) != 0 {
		t.Errorf("чтений базы %d, Set %d, Del %d; want 2, 0, 0", store.reads, len(cache.sets), len(cache.dels))
	}
}

// Испорченная запись в кеше — промах: читаем базу и перезаписываем.
func TestHiddenCorruptEntry(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple)
	cache.data["product:1"] = `{"id":1,"name":`
	cat := NewCatalog(cache, store, Config{TTL: time.Minute})
	p, err := cat.Product(context.Background(), 1)
	if err != nil || p != apple {
		t.Fatalf("Product = %+v, %v; want %+v", p, err, apple)
	}
	if store.reads != 1 || len(cache.sets) != 1 || decode(t, cache.sets[0].val) != apple {
		t.Errorf("чтений базы %d, Set %+v; want 1 и перезапись правильным JSON", store.reads, cache.sets)
	}
}

// Запись, положенная другим экземпляром сервиса, читается без базы.
func TestHiddenReadsForeignEntry(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore()
	cache.data["eu:product:9"] = `{"id":9,"name":"сливы","price":80}`
	cat := NewCatalog(cache, store, Config{Prefix: "eu:", TTL: time.Minute})
	p, err := cat.Product(context.Background(), 9)
	want := Product{ID: 9, Name: "сливы", Price: 80}
	if err != nil || p != want || store.reads != 0 {
		t.Errorf("Product = %+v, %v, чтений базы %d; want %+v без базы", p, err, store.reads, want)
	}
}

func TestHiddenSaveErrors(t *testing.T) {
	cache, store := newFakeCache(), newFakeStore(apple)
	cat := NewCatalog(cache, store, Config{TTL: time.Minute})
	saveErr := errors.New("deadlock")
	store.saveErr = saveErr
	if err := cat.Save(context.Background(), apple); !errors.Is(err, saveErr) {
		t.Fatalf("Save = %v, want ошибку базы", err)
	}
	if len(cache.dels) != 0 {
		t.Fatalf("база не записала, а Del вызван: %v", cache.dels)
	}
	store.saveErr = nil
	delErr := errors.New("valkey timeout")
	cache.delErr = delErr
	err := cat.Save(context.Background(), apple)
	if !errors.Is(err, ErrInvalidate) || !errors.Is(err, delErr) {
		t.Errorf("Save при сбое Del = %v, want ошибку с ErrInvalidate и исходной ошибкой", err)
	}
}

func TestHiddenContextPassed(t *testing.T) {
	type key struct{}
	ctx := context.WithValue(context.Background(), key{}, "req-1")
	cache, store := newFakeCache(), newFakeStore(apple)
	cat := NewCatalog(cache, store, Config{TTL: time.Minute, NegativeTTL: time.Second})
	cat.Product(ctx, 1)
	cat.Product(ctx, 404)
	cat.Save(ctx, apple)
	for _, c := range append(cache.ctxs, store.ctxs...) {
		if c.Value(key{}) != "req-1" {
			t.Fatal("в кеш или базу передан не контекст вызова")
		}
	}
	if len(cache.ctxs) == 0 || len(store.ctxs) == 0 {
		t.Fatal("кеш или база не вызывались")
	}
}

// valkeyCache — Cache поверх valkey-go: настоящий Valkey профиля labs
// (VALKEY_ADDR) или miniredis в памяти.
type valkeyCache struct{ c valkey.Client }

func (v valkeyCache) Get(ctx context.Context, key string) (string, bool, error) {
	s, err := v.c.Do(ctx, v.c.B().Get().Key(key).Build()).ToString()
	if valkey.IsValkeyNil(err) {
		return "", false, nil
	}
	return s, err == nil, err
}

func (v valkeyCache) Set(ctx context.Context, key, val string, ttl time.Duration) error {
	return v.c.Do(ctx, v.c.B().Set().Key(key).Value(val).PxMilliseconds(ttl.Milliseconds()).Build()).Error()
}

func (v valkeyCache) Del(ctx context.Context, key string) error {
	return v.c.Do(ctx, v.c.B().Del().Key(key).Build()).Error()
}

func newValkey(t *testing.T) (valkey.Client, string) {
	t.Helper()
	opt := valkey.ClientOption{DisableCache: true, ForceSingleClient: true}
	prefix := os.Getenv("VALKEY_PREFIX")
	if addr := os.Getenv("VALKEY_ADDR"); addr != "" {
		opt.InitAddress = []string{addr}
		opt.Username = os.Getenv("VALKEY_USERNAME")
		opt.Password = os.Getenv("VALKEY_PASSWORD")
	} else {
		mr := miniredis.RunT(t)
		opt.InitAddress, prefix = []string{mr.Addr()}, "test:"
	}
	c, err := valkey.NewClient(opt)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	return c, prefix
}

func TestHiddenWithValkey(t *testing.T) {
	ctx := context.Background()
	vk, prefix := newValkey(t)
	store := newFakeStore(apple)
	cat := NewCatalog(valkeyCache{vk}, store, Config{Prefix: prefix, TTL: time.Minute, NegativeTTL: 10 * time.Second,
		Jitter: func() time.Duration { return 5 * time.Second }})
	for range 2 {
		if p, err := cat.Product(ctx, 1); err != nil || p != apple {
			t.Fatalf("Product = %+v, %v", p, err)
		}
		if _, err := cat.Product(ctx, 404); !errors.Is(err, ErrNotFound) {
			t.Fatalf("err = %v, want ErrNotFound", err)
		}
	}
	if store.reads != 2 {
		t.Errorf("чтений базы %d, want 2", store.reads)
	}
	pttl := func(id int64) int64 {
		ms, err := vk.Do(ctx, vk.B().Pttl().Key(cat.Key(id)).Build()).AsInt64()
		if err != nil {
			t.Fatal(err)
		}
		return ms
	}
	if ms := pttl(1); ms <= 60_000 || ms > 65_000 {
		t.Errorf("PTTL товара = %d мс, want TTL + Jitter = 65 с", ms)
	}
	if ms := pttl(404); ms <= 0 || ms > 10_000 {
		t.Errorf("PTTL отрицательной записи = %d мс, want до 10 с", ms)
	}
	raw, err := vk.Do(ctx, vk.B().Get().Key(prefix+"product:1").Build()).ToString()
	if err != nil || decode(t, raw) != apple {
		t.Errorf("в Valkey по ключу %sproduct:1: %q, %v", prefix, raw, err)
	}
	if err := cat.Save(ctx, Product{ID: 1, Name: "яблоки", Price: 99}); err != nil {
		t.Fatal(err)
	}
	if n, _ := vk.Do(ctx, vk.B().Exists().Key(cat.Key(1)).Build()).AsInt64(); n != 0 {
		t.Errorf("после Save ключ остался в Valkey")
	}
	vk.Do(ctx, vk.B().Del().Key(cat.Key(404)).Build())
}
