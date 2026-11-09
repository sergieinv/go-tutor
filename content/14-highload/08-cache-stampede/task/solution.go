package task

import (
	"context"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

type Product struct {
	SKU   string
	Name  string
	Price int64
}

type Loader func(ctx context.Context, sku string) (Product, error)

type Config struct {
	TTL         time.Duration
	LoadTimeout time.Duration
	Now         func() time.Time
}

type entry struct {
	p        Product
	loadedAt time.Time
}

type Cache struct {
	load Loader
	cfg  Config
	mu   sync.Mutex
	m    map[string]entry
	g    singleflight.Group
}

func New(load Loader, cfg Config) *Cache {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Cache{load: load, cfg: cfg, m: make(map[string]entry)}
}

func (c *Cache) fresh(sku string) (Product, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[sku]
	if !ok || !c.cfg.Now().Before(e.loadedAt.Add(c.cfg.TTL)) {
		return Product{}, false
	}
	return e.p, true
}

func (c *Cache) Get(ctx context.Context, sku string) (Product, error) {
	if p, ok := c.fresh(sku); ok {
		return p, nil
	}
	if err := ctx.Err(); err != nil {
		return Product{}, err
	}
	// Загрузка общая: отмена того, кто её начал, не должна обрывать её
	// для остальных. Значения контекста сохраняются, отмена — нет.
	lctx := context.WithoutCancel(ctx)
	ch := c.g.DoChan(sku, func() (any, error) {
		// Пока мы шли сюда, предыдущая загрузка могла закончиться.
		if p, ok := c.fresh(sku); ok {
			return p, nil
		}
		ctx := lctx
		if c.cfg.LoadTimeout > 0 {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(lctx, c.cfg.LoadTimeout)
			defer cancel()
		}
		p, err := c.load(ctx, sku)
		if err == nil {
			c.mu.Lock()
			c.m[sku] = entry{p: p, loadedAt: c.cfg.Now()}
			c.mu.Unlock()
		}
		return p, err
	})
	select {
	case r := <-ch:
		return r.Val.(Product), r.Err
	case <-ctx.Done():
		return Product{}, ctx.Err()
	}
}
