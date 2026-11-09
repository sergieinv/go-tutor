// Каталог товаров за распределённым кешем Valkey по схеме cache-aside:
// читаем кеш, при промахе идём в базу и кладём результат с TTL. Плюс
// отрицательное кеширование, джиттер TTL, инвалидация при записи
// и работа без Valkey. С профилем labs пример ходит в настоящий Valkey,
// без него поднимает miniredis в процессе — вывод одинаковый.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/valkey-io/valkey-go"
)

// Адрес и учётные данные — из окружения. `DisableCache` выключает
// клиентский кеш (RESP3 tracking): miniredis его не умеет, и в уроке он
// не нужен. `ForceSingleClient` — один узел без кластерного режима:
// иначе valkey-go сначала пробует CLUSTER-команды, и miniredis на них
// отвечает так, будто он кластер.
func connect() (valkey.Client, string, func(), error) {
	opt := valkey.ClientOption{
		DisableCache:      true,
		ForceSingleClient: true,
		ConnWriteTimeout:  time.Second,
	}
	prefix, stop := os.Getenv("VALKEY_PREFIX"), func() {}
	if addr := os.Getenv("VALKEY_ADDR"); addr != "" {
		opt.InitAddress = []string{addr}
		opt.Username = os.Getenv("VALKEY_USERNAME")
		opt.Password = os.Getenv("VALKEY_PASSWORD")
	} else {
		mr, err := miniredis.Run()
		if err != nil {
			return nil, "", nil, err
		}
		opt.InitAddress, prefix, stop = []string{mr.Addr()}, "lesson:", mr.Close
	}
	c, err := valkey.NewClient(opt)
	if err != nil {
		stop()
		return nil, "", nil, err
	}
	return c, prefix, func() { c.Close(); stop() }, nil
}

type Product struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Price int64  `json:"price"`
}

var ErrNotFound = errors.New("товар не найден")

// «База» со счётчиком чтений — по нему видно, помог ли кеш.
type db struct {
	mu    sync.Mutex
	rows  map[int64]Product
	reads int
}

func (d *db) Product(_ context.Context, id int64) (Product, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.reads++
	p, ok := d.rows[id]
	if !ok {
		return Product{}, ErrNotFound
	}
	return p, nil
}

type catalog struct {
	vk     valkey.Client
	db     *db
	prefix string
	ttl    time.Duration
	negTTL time.Duration
}

// В ключе — префикс запуска и версия формата: поменяли структуру
// `Product` — подняли `v1` до `v2`, и старые записи просто не читаются.
func (c *catalog) key(id int64) string {
	return c.prefix + "product:v1:" + strconv.FormatInt(id, 10)
}

// Джиттер разносит истечение записей, записанных одновременно
// (прогрев, всплеск), — иначе они и истекут одновременно.
func (c *catalog) jittered() time.Duration {
	return c.ttl + rand.N(c.ttl/10)
}

func (c *catalog) Product(ctx context.Context, id int64) (Product, string, error) {
	// Кеш — ускоритель, а не источник истины: на него — короткий таймаут,
	// а любая его ошибка означает «иди в базу».
	cctx, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	defer cancel()
	val, err := c.vk.Do(cctx, c.vk.B().Get().Key(c.key(id)).Build()).ToString()
	switch {
	case err == nil && val == "":
		return Product{}, "кеш (отрицательная запись)", ErrNotFound
	case err == nil:
		var p Product
		if json.Unmarshal([]byte(val), &p) == nil {
			return p, "кеш", nil
		}
		// Испорченная запись — как промах: перечитаем и перезапишем.
	case !valkey.IsValkeyNil(err):
		p, err := c.db.Product(ctx, id)
		return p, "база (кеш недоступен)", err
	}

	p, err := c.db.Product(ctx, id)
	switch {
	case errors.Is(err, ErrNotFound):
		// Отрицательная запись — пустая строка с коротким TTL: несуществующий
		// id не будет бить в базу на каждом запросе.
		c.vk.Do(cctx, c.vk.B().Set().Key(c.key(id)).Value("").PxMilliseconds(c.negTTL.Milliseconds()).Build())
		return Product{}, "база", err
	case err != nil:
		return Product{}, "база", err // ошибку базы не кешируем
	}
	b, _ := json.Marshal(p)
	c.vk.Do(cctx, c.vk.B().Set().Key(c.key(id)).Value(string(b)).PxMilliseconds(c.jittered().Milliseconds()).Build())
	return p, "база", nil
}

// Запись: сначала база, потом удаление ключа. Следующее чтение
// возьмёт свежее значение из базы и положит его в кеш.
func (c *catalog) UpdatePrice(ctx context.Context, id, price int64) error {
	c.db.mu.Lock()
	p := c.db.rows[id]
	p.Price = price
	c.db.rows[id] = p
	c.db.mu.Unlock()
	return c.vk.Do(ctx, c.vk.B().Del().Key(c.key(id)).Build()).Error()
}

func main() {
	ctx := context.Background()
	vk, prefix, stop, err := connect()
	if err != nil {
		log.Fatal(err)
	}
	defer stop()

	store := &db{rows: map[int64]Product{1: {ID: 1, Name: "яблоки", Price: 120}}}
	cat := &catalog{vk: vk, db: store, prefix: prefix, ttl: time.Minute, negTTL: 10 * time.Second}
	show := func(id int64) {
		p, from, err := cat.Product(ctx, id)
		if err != nil {
			fmt.Printf("товар %d: %v — %s, чтений базы: %d\n", id, err, from, store.reads)
			return
		}
		fmt.Printf("товар %d: %s за %d — %s, чтений базы: %d\n", id, p.Name, p.Price, from, store.reads)
	}

	fmt.Println("cache-aside:")
	show(1)
	show(1)
	fmt.Println("отрицательное кеширование:")
	show(404)
	show(404)

	// TTL выставлен, джиттер — только в плюс, до 10%. Точное значение
	// от запуска к запуску разное, поэтому печатаем только проверку.
	ttl, _ := vk.Do(ctx, vk.B().Pttl().Key(cat.key(1)).Build()).AsInt64()
	neg, _ := vk.Do(ctx, vk.B().Pttl().Key(cat.key(404)).Build()).AsInt64()
	fmt.Println("TTL товара от 59 до 66 с:", ttl > 59_000 && ttl <= 66_000)
	fmt.Println("TTL отрицательной записи до 10 с:", neg > 0 && neg <= 10_000)

	fmt.Println("инвалидация при записи:")
	if err := cat.UpdatePrice(ctx, 1, 99); err != nil {
		log.Fatal(err)
	}
	show(1)
	show(1)

	// Valkey упал: закрытый клиент возвращает ошибку на любую команду.
	// Каталог деградирует в базу, а не отдаёт пользователю 500.
	fmt.Println("Valkey недоступен:")
	broken, _, stopBroken, err := connect()
	if err != nil {
		log.Fatal(err)
	}
	stopBroken()
	cat.vk = broken
	show(1)
}
