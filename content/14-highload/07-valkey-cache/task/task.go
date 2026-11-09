package task

import (
	"context"
	"errors"
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

// Cache — то, что каталогу нужно от Valkey. ok == false — ключа нет;
// err != nil — кеш недоступен или ответил ошибкой.
type Cache interface {
	Get(ctx context.Context, key string) (val string, ok bool, err error)
	Set(ctx context.Context, key, val string, ttl time.Duration) error
	Del(ctx context.Context, key string) error
}

// Store — база товаров. Product возвращает ErrNotFound, если товара нет.
type Store interface {
	Product(ctx context.Context, id int64) (Product, error)
	SaveProduct(ctx context.Context, p Product) error
}

type Config struct {
	Prefix      string               // префикс всех ключей
	TTL         time.Duration        // срок записи о товаре
	NegativeTTL time.Duration        // срок записи «товара нет»; <= 0 — не кешировать
	Jitter      func() time.Duration // добавка к TTL; nil — без добавки
}

type Catalog struct {
	// TODO
}

func NewCatalog(c Cache, s Store, cfg Config) *Catalog {
	// TODO
	return &Catalog{}
}

func (c *Catalog) Key(id int64) string {
	// TODO
	return ""
}

func (c *Catalog) Product(ctx context.Context, id int64) (Product, error) {
	// TODO: cache-aside с отрицательным кешированием
	return Product{}, nil
}

func (c *Catalog) Save(ctx context.Context, p Product) error {
	// TODO: записать в базу, затем инвалидировать кеш
	return nil
}
