package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"time"
)

var (
	ErrNotFound   = errors.New("product not found")
	ErrInvalidate = errors.New("cache invalidation failed")
)

type Product struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Price int64  `json:"price"`
}

type Cache interface {
	Get(ctx context.Context, key string) (val string, ok bool, err error)
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
}

type Store interface {
	Product(ctx context.Context, id int64) (Product, error)
	SaveProduct(ctx context.Context, p Product) error
}

type Config struct {
	Prefix      string
	TTL         time.Duration
	NegativeTTL time.Duration
	Jitter      func() time.Duration
}

type Catalog struct {
	cache Cache
	store Store
	cfg   Config
}

func NewCatalog(c Cache, s Store, cfg Config) *Catalog {
	if cfg.Jitter == nil {
		cfg.Jitter = func() time.Duration { return 0 }
	}
	return &Catalog{cache: c, store: s, cfg: cfg}
}

func (c *Catalog) Key(id int64) string {
	return c.cfg.Prefix + "product:" + strconv.FormatInt(id, 10)
}

func (c *Catalog) Product(ctx context.Context, id int64) (Product, error) {
	key := c.Key(id)
	val, ok, err := c.cache.Get(ctx, key)
	if err != nil {
		// Кеш недоступен — деградируем в хранилище и кеш не трогаем.
		return c.store.Product(ctx, id)
	}
	if ok {
		if val == "" {
			return Product{}, ErrNotFound
		}
		var p Product
		if json.Unmarshal([]byte(val), &p) == nil {
			return p, nil
		}
		// Испорченная запись — как промах.
	}
	p, err := c.store.Product(ctx, id)
	switch {
	case errors.Is(err, ErrNotFound):
		if c.cfg.NegativeTTL > 0 {
			_ = c.cache.Set(ctx, key, "", c.cfg.NegativeTTL)
		}
		return Product{}, err
	case err != nil:
		return Product{}, err
	}
	b, err := json.Marshal(p)
	if err == nil {
		_ = c.cache.Set(ctx, key, string(b), c.cfg.TTL+c.cfg.Jitter())
	}
	return p, nil
}

func (c *Catalog) Save(ctx context.Context, p Product) error {
	if err := c.store.SaveProduct(ctx, p); err != nil {
		return err
	}
	if err := c.cache.Del(ctx, c.Key(p.ID)); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidate, err)
	}
	return nil
}
