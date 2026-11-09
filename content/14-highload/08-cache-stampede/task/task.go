package task

import (
	"context"
	"time"
)

type Product struct {
	SKU   string
	Name  string
	Price int64
}

// Loader читает товар из источника (базы).
type Loader func(ctx context.Context, sku string) (Product, error)

type Config struct {
	TTL         time.Duration    // сколько запись свежая
	LoadTimeout time.Duration    // > 0 — предел времени одной загрузки
	Now         func() time.Time // nil — time.Now
}

// Cache — кеш каталога с TTL и защитой от stampede (см. «Задание»).
type Cache struct {
	// TODO
}

func New(load Loader, cfg Config) *Cache {
	// TODO
	return &Cache{}
}

func (c *Cache) Get(ctx context.Context, sku string) (Product, error) {
	// TODO: свежая запись — сразу; иначе одна загрузка на ключ для всех ждущих
	return Product{}, nil
}
