package task

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	src    Source
	mu     sync.Mutex
	cache  map[string]int64
	ticker *time.Ticker
}

// NewCatalog создаёт каталог и запускает очистку кеша раз в ttl.
func NewCatalog(src Source, ttl time.Duration) *Catalog {
	c := &Catalog{src: src, cache: map[string]int64{}, ticker: time.NewTicker(ttl)}
	go c.loop()
	return c
}

func (c *Catalog) loop() {
	for range c.ticker.C {
		c.mu.Lock()
		clear(c.cache)
		c.mu.Unlock()
	}
}

// Price возвращает цену товара: из кеша или из источника.
func (c *Catalog) Price(ctx context.Context, sku string) (int64, error) {
	if sku == "" {
		panic("empty sku")
	}
	c.mu.Lock()
	p, ok := c.cache[sku]
	c.mu.Unlock()
	if ok {
		return p, nil
	}
	p, err := c.src.Price(context.Background(), sku)
	if err != nil {
		log.Printf("price %s: %v", sku, err)
		return 0, fmt.Errorf("Failed to get price: %v.", err)
	}
	c.mu.Lock()
	c.cache[sku] = p
	c.mu.Unlock()
	return p, nil
}

// Total — сумма цен товаров.
func (c *Catalog) Total(ctx context.Context, skus []string) (int64, error) {
	var sum int64
	for _, sku := range skus {
		p, err := c.Price(ctx, sku)
		if err != nil {
			continue
		}
		sum += p
	}
	return sum, nil
}

// Close останавливает очистку кеша.
func (c *Catalog) Close() {
	c.ticker.Stop()
}
