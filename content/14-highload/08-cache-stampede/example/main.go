// Cache stampede: у горячего ключа истёк TTL, и сотня запросов
// одновременно идёт в базу за одним и тем же значением. `singleflight`
// превращает сотню загрузок в одну, `DoChan` даёт ждущему уйти по отмене,
// а XFetch обновляет запись заранее, чтобы промаха не было вовсе.
// «База» ждёт на канале `gate`, поэтому вывод не зависит от планировщика.
package main

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
)

type db struct {
	calls    atomic.Int64
	inflight atomic.Int64
	gate     chan struct{} // база отвечает, когда канал закрыт
	fail     error
}

func (d *db) load(ctx context.Context, sku string) (string, error) {
	d.calls.Add(1)
	d.inflight.Add(1)
	defer d.inflight.Add(-1)
	select {
	case <-d.gate:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if d.fail != nil {
		return "", d.fail
	}
	return "карточка " + sku, nil
}

// Кеш без срока — только чтобы показать промах; TTL и его истечение —
// в задании урока.
type cache struct {
	mu sync.Mutex
	m  map[string]string
	g  singleflight.Group
	db *db
	// joined — сколько вызовов `DoChan` уже ждут результат (для вывода).
	joined atomic.Int64
}

func (c *cache) lookup(sku string) (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.m[sku]
	return v, ok
}

func (c *cache) store(sku, v string) { c.mu.Lock(); c.m[sku] = v; c.mu.Unlock() }

// Наивно: промах — сразу в базу. Каждый промах — свой запрос.
func (c *cache) getNaive(ctx context.Context, sku string) (string, error) {
	if v, ok := c.lookup(sku); ok {
		return v, nil
	}
	v, err := c.db.load(ctx, sku)
	if err == nil {
		c.store(sku, v)
	}
	return v, err
}

// С `singleflight`: пока загрузка ключа идёт, остальные вызовы `Do`
// с тем же ключом ждут её и получают тот же результат — и значение,
// и ошибку. Внутри — повторная проверка кеша: горутина могла увидеть
// промах до того, как предыдущая загрузка закончилась.
func (c *cache) get(ctx context.Context, sku string) (string, error) {
	if v, ok := c.lookup(sku); ok {
		return v, nil
	}
	v, err, _ := c.g.Do(sku, func() (any, error) {
		if v, ok := c.lookup(sku); ok {
			return v, nil
		}
		v, err := c.db.load(ctx, sku)
		if err == nil {
			c.store(sku, v)
		}
		return v, err
	})
	return v.(string), err
}

// `DoChan` + `select`: ждущий уходит по своему контексту, а загрузка
// продолжается для остальных. Она идёт с `context.WithoutCancel` —
// иначе отмена того, кто её начал, оборвала бы её для всех.
func (c *cache) getCtx(ctx context.Context, sku string) (string, error) {
	if v, ok := c.lookup(sku); ok {
		return v, nil
	}
	lctx := context.WithoutCancel(ctx)
	ch := c.g.DoChan(sku, func() (any, error) {
		v, err := c.db.load(lctx, sku)
		if err == nil {
			c.store(sku, v)
		}
		return v, err
	})
	c.joined.Add(1)
	select {
	case r := <-ch:
		return r.Val.(string), r.Err
	case <-ctx.Done():
		return "", ctx.Err()
	}
}

// crowd запускает n запросов за одним ключом и ждёт, пока в базу придёт
// `want` загрузок, а для `DoChan` — ещё и пока все n встанут в ожидание;
// потом «база» отвечает.
func crowd(c *cache, n int, want int64, get func(context.Context, string) (string, error), viaChan bool) (ok, failed int) {
	c.joined.Store(0)
	c.db.gate = make(chan struct{})
	var mu sync.Mutex
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			_, err := get(context.Background(), "apple")
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				failed++
			} else {
				ok++
			}
		})
	}
	for c.db.inflight.Load() < want || viaChan && c.joined.Load() < int64(n) {
		time.Sleep(time.Millisecond)
	}
	close(c.db.gate)
	wg.Wait()
	return ok, failed
}

func main() {
	d := &db{}
	c := &cache{m: map[string]string{}, db: d}

	ok, _ := crowd(c, 100, 100, c.getNaive, false)
	fmt.Printf("без защиты: 100 запросов, ответов %d, загрузок из базы %d\n", ok, d.calls.Load())

	delete(c.m, "apple") // TTL истёк
	d.calls.Store(0)
	ok, _ = crowd(c, 100, 1, c.get, false)
	fmt.Printf("singleflight: 100 запросов, ответов %d, загрузок из базы %d\n", ok, d.calls.Load())

	// Ошибка тоже общая — и её не кешируют: следующий запрос пробует снова.
	delete(c.m, "apple")
	d.calls.Store(0)
	d.fail = errors.New("база: слишком много соединений")
	_, failed := crowd(c, 10, 1, c.getCtx, true)
	fmt.Printf("ошибка базы: 10 запросов, ошибок %d, загрузок %d\n", failed, d.calls.Load())
	d.fail = nil
	crowd(c, 1, 1, c.get, false)
	fmt.Printf("следующий запрос снова идёт в базу: загрузок %d\n", d.calls.Load())

	// Тот, кто начал загрузку, отменён — остальные всё равно получают значение.
	delete(c.m, "apple")
	d.calls.Store(0)
	d.gate = make(chan struct{})
	c.joined.Store(0)
	ctx, cancel := context.WithCancel(context.Background())
	first := make(chan error, 1)
	go func() { _, err := c.getCtx(ctx, "apple"); first <- err }()
	for d.inflight.Load() < 1 {
		time.Sleep(time.Millisecond)
	}
	second := make(chan string, 1)
	go func() { v, _ := c.getCtx(context.Background(), "apple"); second <- v }()
	for c.joined.Load() < 2 {
		time.Sleep(time.Millisecond)
	}
	cancel()
	fmt.Println("начавший загрузку отменён:", <-first)
	close(d.gate)
	fmt.Printf("второй получил: %q, загрузок %d\n", <-second, d.calls.Load())

	// XFetch: запрос обновляет запись раньше срока с вероятностью, растущей
	// к его концу: обновить, если delta·beta·(−ln U) ≥ осталось до срока.
	// delta — сколько длится загрузка (здесь 1 с), beta = 1. Генератор
	// с фиксированным seed — чтобы вывод был одинаковым.
	r := rand.New(rand.NewPCG(1, 2))
	delta, beta := time.Second, 1.0
	fmt.Println("XFetch: сколько из 1000 запросов обновят запись заранее")
	for _, left := range []time.Duration{10 * time.Second, 5 * time.Second, 2 * time.Second, time.Second, 200 * time.Millisecond} {
		n := 0
		for range 1000 {
			if float64(delta)*beta*-math.Log(1-r.Float64()) >= float64(left) {
				n++
			}
		}
		fmt.Printf("  до истечения %v: %d\n", left, n)
	}
}
