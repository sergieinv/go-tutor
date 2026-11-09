// Package catalog — кеш каталога товаров в Valkey (модуль 14).
//
// Каталог читается на каждом CreateOrder и GetProduct, а меняется редко:
// классический случай для cache-aside. Cache реализует orders.Catalog
// поверх источника (базы) и KV (Valkey):
//
//   - cache-aside: сначала MGET из Valkey, промахи — из базы, результат —
//     обратно в Valkey с TTL;
//   - TTL с джиттером (±Jitter): записи, загруженные одной волной (после
//     старта или очистки кеша), не истекают в одну и ту же секунду;
//   - защита от stampede: одновременные промахи по одному ключу идут в
//     базу одним запросом (singleflight на ключ), остальные ждут его;
//   - отрицательное кеширование: «товара нет» тоже кешируется, но коротко
//     (NegativeTTL) — запросы несуществующих id не долбят базу;
//   - деградация: Valkey недоступен или медленный (OpTimeout) — читаем из
//     базы и не падаем; ошибки кеша видны в метрике.
//
// Инвалидации при изменении цены нет: каталогом «Заказы» через API не
// управляют, цена обновится по TTL. Цена в заказе всё равно фиксируется на
// момент заказа, а склад проверяет остатки сам — устаревшая на минуты цена
// здесь допустима. Если бы не была — удаляли бы ключ после изменения в
// базе (а лучше — по событию из outbox).
package catalog

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"golang.org/x/sync/singleflight"

	"gotutor/labs/orders/internal/orders"
)

// KV — то, что кешу нужно от Valkey. Реализация — Valkey (valkey-go);
// в тестах — она же поверх miniredis.
type KV interface {
	// MGet возвращает значения ключей в том же порядке; отсутствующий
	// ключ — nil.
	MGet(ctx context.Context, keys []string) ([]*string, error)
	// Set записывает значение с временем жизни.
	Set(ctx context.Context, key, value string, ttl time.Duration) error
}

// Config — настройки кеша.
type Config struct {
	// Prefix — префикс ключей; версия в нём позволяет сменить формат
	// значения, не читая старые записи. Пусто — "orders:catalog:v1:".
	Prefix string
	// TTL — время жизни записи о товаре; 0 — 5 минут.
	TTL time.Duration
	// Jitter — разброс TTL в долях: 0.1 — ±10%. 0 — 0.1; отрицательный —
	// без разброса.
	Jitter float64
	// NegativeTTL — время жизни записи «товара нет»; 0 — 30 секунд.
	NegativeTTL time.Duration
	// OpTimeout — на одну операцию с Valkey; 0 — 100 мс. Кеш, который
	// отвечает медленнее базы, хуже, чем никакого.
	OpTimeout time.Duration
	// LoadTimeout — на загрузку из базы при промахе; 0 — 5 секунд.
	LoadTimeout time.Duration
	// Enabled — выключатель (разбор нагрузки, аварийное отключение); nil —
	// всегда включён. Выключенный кеш не читает и не пишет Valkey.
	Enabled func() bool
	// Registerer — куда зарегистрировать метрики; nil — без метрик.
	Registerer prometheus.Registerer
	Logger     *slog.Logger
	// Rand — источник случайности для джиттера [0, 1); nil — math/rand/v2.
	Rand func() float64
}

// Cache — каталог с кешем. Реализует orders.Catalog.
type Cache struct {
	kv    KV
	src   orders.Catalog
	cfg   Config
	group singleflight.Group
	m     *metrics

	logMu   sync.Mutex
	lastLog time.Time
}

var _ orders.Catalog = (*Cache)(nil)

// negative — значение записи «товара нет». JSON товара никогда не равен
// ему, поэтому отдельный тип записи не нужен.
const negative = "-"

// New создаёт кеш поверх источника src.
func New(kv KV, src orders.Catalog, cfg Config) *Cache {
	if cfg.Prefix == "" {
		cfg.Prefix = "orders:catalog:v1:"
	}
	if cfg.TTL == 0 {
		cfg.TTL = 5 * time.Minute
	}
	if cfg.Jitter == 0 {
		cfg.Jitter = 0.1
	}
	if cfg.NegativeTTL == 0 {
		cfg.NegativeTTL = 30 * time.Second
	}
	if cfg.OpTimeout == 0 {
		cfg.OpTimeout = 100 * time.Millisecond
	}
	if cfg.LoadTimeout == 0 {
		cfg.LoadTimeout = 5 * time.Second
	}
	if cfg.Enabled == nil {
		cfg.Enabled = func() bool { return true }
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.New(slog.DiscardHandler)
	}
	if cfg.Rand == nil {
		cfg.Rand = rand.Float64
	}
	return &Cache{kv: kv, src: src, cfg: cfg, m: newMetrics(cfg.Registerer)}
}

// cachedProduct — значение в Valkey. Свой тип, а не orders.Product:
// формат хранения не должен меняться от переименования поля в домене.
type cachedProduct struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	PriceCents int64  `json:"price_cents"`
}

// ProductsByIDs возвращает найденные товары, упорядоченные по ID.
func (c *Cache) ProductsByIDs(ctx context.Context, ids []int64) ([]orders.Product, error) {
	if !c.cfg.Enabled() {
		c.m.lookups.WithLabelValues("bypass").Add(float64(len(ids)))
		return c.src.ProductsByIDs(ctx, ids)
	}
	ids = uniqueSorted(ids)
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = c.key(id)
	}

	kvOK := true
	opCtx, cancel := context.WithTimeout(ctx, c.cfg.OpTimeout)
	cached, err := c.kv.MGet(opCtx, keys)
	cancel()
	if err != nil || len(cached) != len(keys) {
		// Деградация: кеш недоступен — все id из базы. singleflight
		// остаётся: без кеша защита базы от толпы одинаковых запросов
		// нужнее всего. Записывать в недоступный кеш не пытаемся.
		kvOK = false
		cached = make([]*string, len(keys))
		c.m.lookups.WithLabelValues("error").Add(float64(len(ids)))
		c.logKVError(ctx, "catalog cache read failed, reading from db", err)
	}

	products := make([]orders.Product, 0, len(ids))
	var misses []int64
	for i, v := range cached {
		switch {
		case v == nil:
			if kvOK {
				c.m.lookups.WithLabelValues("miss").Inc()
			}
			misses = append(misses, ids[i])
		case *v == negative:
			c.m.lookups.WithLabelValues("negative_hit").Inc()
		default:
			var p cachedProduct
			if err := json.Unmarshal([]byte(*v), &p); err != nil || p.ID != ids[i] {
				// Испорченная или чужая запись — как промах; загрузка
				// перезапишет её.
				c.m.lookups.WithLabelValues("miss").Inc()
				misses = append(misses, ids[i])
				continue
			}
			c.m.lookups.WithLabelValues("hit").Inc()
			products = append(products, orders.Product{ID: p.ID, Name: p.Name, PriceCents: p.PriceCents})
		}
	}
	if len(misses) == 0 {
		return products, nil
	}

	loaded, err := c.loadMisses(ctx, misses, kvOK)
	if err != nil {
		return nil, err
	}
	products = append(products, loaded...)
	slices.SortFunc(products, func(a, b orders.Product) int { return cmp.Compare(a.ID, b.ID) })
	return products, nil
}

// loadResult — итог загрузки одного id: товар или «нет такого».
type loadResult struct {
	product orders.Product
	found   bool
}

// loadMisses загружает промахи из базы, по одному singleflight на ключ.
//
// Каждый промах — отдельный запрос к базе (WHERE id = ANY('{id}')), а не
// один на все: так одинаковые ключи из разных корзин склеиваются, а
// промахи случаются раз в TTL на товар. Загрузки идут параллельно.
func (c *Cache) loadMisses(ctx context.Context, ids []int64, store bool) ([]orders.Product, error) {
	chans := make([]<-chan singleflight.Result, len(ids))
	// leader[i] — загрузку ключа запустил этот вызов (fn выполняется только
	// у первого); остальные получили чужой результат. Чтение после приёма из
	// канала синхронизировано с записью в fn.
	leader := make([]bool, len(ids))
	for i, id := range ids {
		chans[i] = c.group.DoChan(c.key(id), func() (any, error) {
			leader[i] = true
			return c.load(ctx, id, store)
		})
	}
	var products []orders.Product
	for i, ch := range chans {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case res := <-ch:
			if res.Err != nil {
				return nil, res.Err
			}
			if !leader[i] {
				c.m.coalesced.Inc()
			}
			if r := res.Val.(loadResult); r.found {
				products = append(products, r.product)
			}
		}
	}
	return products, nil
}

// load — тело singleflight: база → Valkey.
//
// Контекст загрузки отвязан от запроса, который её начал: результат ждут
// и другие запросы, и уход первого клиента не должен ронять их всех.
func (c *Cache) load(ctx context.Context, id int64, store bool) (loadResult, error) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), c.cfg.LoadTimeout)
	defer cancel()
	ps, err := c.src.ProductsByIDs(ctx, []int64{id})
	if err != nil {
		return loadResult{}, fmt.Errorf("load product %d: %w", id, err)
	}
	var res loadResult
	value, ttl := negative, c.cfg.NegativeTTL
	if len(ps) > 0 {
		res = loadResult{product: ps[0], found: true}
		b, _ := json.Marshal(cachedProduct{ID: ps[0].ID, Name: ps[0].Name, PriceCents: ps[0].PriceCents})
		value, ttl = string(b), c.jittered(c.cfg.TTL)
	}
	if store {
		opCtx, cancel := context.WithTimeout(ctx, c.cfg.OpTimeout)
		defer cancel()
		// Ошибка записи не мешает ответу: в следующий раз будет промах.
		if err := c.kv.Set(opCtx, c.key(id), value, ttl); err != nil {
			c.m.lookups.WithLabelValues("error").Inc()
			c.logKVError(ctx, "catalog cache write failed", err)
		}
	}
	return res, nil
}

// jittered возвращает d ± Jitter·d.
func (c *Cache) jittered(d time.Duration) time.Duration {
	if c.cfg.Jitter <= 0 {
		return d
	}
	delta := (c.cfg.Rand()*2 - 1) * c.cfg.Jitter * float64(d)
	return max(time.Second, d+time.Duration(delta))
}

func (c *Cache) key(id int64) string { return c.cfg.Prefix + strconv.FormatInt(id, 10) }

// logKVError пишет ошибку кеша не чаще раза в 10 секунд: при лежащем Valkey
// иначе была бы строка журнала на каждый запрос. Счёт ошибок — в метрике.
func (c *Cache) logKVError(ctx context.Context, msg string, err error) {
	if err == nil {
		err = errors.New("unexpected MGET reply length")
	}
	c.logMu.Lock()
	now := time.Now()
	if now.Sub(c.lastLog) < 10*time.Second {
		c.logMu.Unlock()
		return
	}
	c.lastLog = now
	c.logMu.Unlock()
	c.cfg.Logger.WarnContext(ctx, msg, "err", err)
}

func uniqueSorted(ids []int64) []int64 {
	ids = slices.Clone(ids)
	slices.Sort(ids)
	return slices.Compact(ids)
}

type metrics struct {
	lookups   *prometheus.CounterVec
	coalesced prometheus.Counter
}

func newMetrics(reg prometheus.Registerer) *metrics {
	m := &metrics{
		lookups: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "orders_catalog_cache_lookups_total",
			Help: "Catalog cache lookups per product key: hit, negative_hit, miss, error (Valkey unavailable), bypass (cache switched off).",
		}, []string{"result"}),
		coalesced: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "orders_catalog_cache_coalesced_total",
			Help: "Cache misses that waited for a load already in flight (singleflight) instead of querying the db.",
		}),
	}
	if reg != nil {
		reg.MustRegister(m.lookups, m.coalesced)
	}
	return m
}

// Counted считает запросы к источнику каталога (базе) — метрика
// orders_catalog_db_queries_total. Ставится между кешем и базой, поэтому
// видно, сколько запросов кеш снял с базы.
func Counted(src orders.Catalog, reg prometheus.Registerer) orders.Catalog {
	c := countedCatalog{src: src, queries: prometheus.NewCounter(prometheus.CounterOpts{
		Name: "orders_catalog_db_queries_total",
		Help: "Catalog queries that reached the database.",
	})}
	if reg != nil {
		reg.MustRegister(c.queries)
	}
	return c
}

type countedCatalog struct {
	src     orders.Catalog
	queries prometheus.Counter
}

func (c countedCatalog) ProductsByIDs(ctx context.Context, ids []int64) ([]orders.Product, error) {
	c.queries.Inc()
	return c.src.ProductsByIDs(ctx, ids)
}
