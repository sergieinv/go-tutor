package task

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	ErrNotFound   = errors.New("not found")
	ErrInvalidSKU = errors.New("invalid sku")
)

// Source — откуда каталог берёт цены (в сервисе — база).
type Source interface {
	Price(ctx context.Context, sku string) (int64, error)
}

// Catalog — цены с кешем, который периодически очищается.
type Catalog struct {
	src   Source
	mu    sync.Mutex
	cache map[string]int64
	done  chan struct{}
	wg    sync.WaitGroup
}

// NewCatalog создаёт каталог и запускает очистку кеша раз в ttl.
// Горутиной владеет каталог: Close её останавливает и дожидается.
func NewCatalog(src Source, ttl time.Duration) *Catalog {
	c := &Catalog{src: src, cache: map[string]int64{}, done: make(chan struct{})}
	c.wg.Go(func() { c.loop(ttl) })
	return c
}

func (c *Catalog) loop(ttl time.Duration) {
	t := time.NewTicker(ttl)
	defer t.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			c.mu.Lock()
			clear(c.cache)
			c.mu.Unlock()
		}
	}
}

// Price возвращает цену товара: из кеша или из источника.
func (c *Catalog) Price(ctx context.Context, sku string) (int64, error) {
	if sku == "" {
		return 0, ErrInvalidSKU
	}
	c.mu.Lock()
	p, ok := c.cache[sku]
	c.mu.Unlock()
	if ok {
		return p, nil
	}
	p, err := c.src.Price(ctx, sku)
	if err != nil {
		return 0, fmt.Errorf("price %q: %w", sku, err)
	}
	c.mu.Lock()
	c.cache[sku] = p
	c.mu.Unlock()
	return p, nil
}

// Total — сумма цен товаров; первая ошибка прерывает подсчёт.
func (c *Catalog) Total(ctx context.Context, skus []string) (int64, error) {
	var sum int64
	for _, sku := range skus {
		p, err := c.Price(ctx, sku)
		if err != nil {
			return 0, fmt.Errorf("total: %w", err)
		}
		sum += p
	}
	return sum, nil
}

// Close останавливает очистку кеша и ждёт, пока горутина выйдет.
func (c *Catalog) Close() {
	close(c.done)
	c.wg.Wait()
}
