// База под нагрузкой: сколько запросов делает обработчик, сколько
// строк читает каждый, сколько ждёт пула и чем обрывается долгий
// запрос. Всё печатается счётчиками и булевыми значениями — время
// и стоимости меняются от запуска к запуску.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgconn/ctxwatch"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	// Без профиля labs переменная пуста — сообщаем и выходим.
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		fmt.Println("нужен PostgreSQL из профиля labs: docker compose --profile labs up")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// `QueryTracer` вызывается на каждый `Query`, `QueryRow` и
	// `Exec` — так в сервисе считают запросы на обработчик
	// (метрика или проверка в тесте).
	cfg, err := pgxpool.ParseConfig(dsn)
	check(err)
	cfg.MaxConns = 4
	counter := &queryCounter{}
	cfg.ConnConfig.Tracer = counter
	// Параллельные планы делят строки между воркерами — отключаем
	// для всех соединений пула (не `SET`: он попал бы в одно).
	cfg.ConnConfig.RuntimeParams["max_parallel_workers_per_gather"] = "0"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	check(err)
	defer pool.Close()
	exec(ctx, pool, `CREATE TABLE customers (id bigint PRIMARY KEY, name text NOT NULL);
	INSERT INTO customers SELECT g, 'покупатель ' || g FROM generate_series(1, 1000) g;
	CREATE TABLE orders (
		id          bigint PRIMARY KEY,
		customer_id bigint NOT NULL REFERENCES customers,
		created_at  timestamptz NOT NULL,
		total       bigint NOT NULL);
	INSERT INTO orders SELECT g, g % 1000 + 1,
		timestamptz '2026-01-01 00:00:00+00' + g * interval '1 minute', g % 5000
	FROM generate_series(1, 50000) g;
	CREATE INDEX orders_feed ON orders (created_at, id)`)
	// VACUUM обновляет карту видимости — без неё план мог бы
	// выбрать обычный Index Scan вместо Index Only Scan.
	exec(ctx, pool, `VACUUM ANALYZE customers, orders`)

	// N+1: лента из 20 заказов и по запросу на имя покупателя.
	// При 500 запросах ленты в секунду это 10 500 запросов к базе
	// в секунду, и каждый держит соединение пула.
	counter.n.Store(0)
	ids := latestCustomers(ctx, pool)
	names := map[int64]string{}
	for _, id := range ids {
		var name string
		check(pool.QueryRow(ctx, `SELECT name FROM customers WHERE id = $1`, id).Scan(&name))
		names[id] = name
	}
	fmt.Println("N+1: запросов", counter.n.Load())

	// То же двумя запросами: все id одним массивом в `= ANY($1)`.
	counter.n.Store(0)
	ids = latestCustomers(ctx, pool)
	rows, _ := pool.Query(ctx, `SELECT id, name FROM customers WHERE id = ANY($1)`, ids)
	batch := map[int64]string{}
	for rows.Next() {
		var id int64
		var name string
		check(rows.Scan(&id, &name))
		batch[id] = name
	}
	check(rows.Err())
	fmt.Println("батч: запросов", counter.n.Load(), "| имена совпали:", fmt.Sprint(names) == fmt.Sprint(batch))

	// Глубокая страница через OFFSET: индекс даёт порядок, но
	// базе всё равно нужно пройти 10 000 строк, чтобы их пропустить.
	page := `SELECT id FROM orders ORDER BY created_at DESC, id DESC`
	explain(ctx, pool, "OFFSET 10000", page+` LIMIT 20 OFFSET 10000`)

	// Keyset: курсор — ключ последней строки предыдущей страницы.
	// Сравнение кортежей `(created_at, id) < ($1, $2)` спускается
	// по индексу сразу к нужному месту — 20 строк на любой странице.
	var at time.Time
	var id int64
	check(pool.QueryRow(ctx, page+` LIMIT 1 OFFSET 9999`).Scan(&id))
	check(pool.QueryRow(ctx, `SELECT created_at FROM orders WHERE id = $1`, id).Scan(&at))
	explain(ctx, pool, "keyset", `SELECT id FROM orders WHERE (created_at, id) < ($1, $2)
		ORDER BY created_at DESC, id DESC LIMIT 20`, at, id)

	poolWaits(ctx, dsn)
	timeouts(ctx, dsn)
}

// latestCustomers — покупатели 20 последних заказов, один запрос.
func latestCustomers(ctx context.Context, pool *pgxpool.Pool) []int64 {
	rows, _ := pool.Query(ctx, `SELECT customer_id FROM orders ORDER BY created_at DESC, id DESC LIMIT 20`)
	ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
	check(err)
	return ids
}

// Пул из двух соединений. Оба заняты — запрос стоит в очереди
// пула, пока не кончится его контекст: таймаут ожидания пула —
// это дедлайн запроса, отдельной настройки у pgxpool нет.
func poolWaits(ctx context.Context, dsn string) {
	cfg, err := pgxpool.ParseConfig(dsn)
	check(err)
	cfg.MaxConns = 2
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	check(err)
	defer pool.Close()
	c1, err := pool.Acquire(ctx)
	check(err)
	c2, err := pool.Acquire(ctx)
	check(err)
	short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
	_, err = pool.Exec(short, `SELECT 1`)
	cancel()
	fmt.Println("пул занят, запрос с дедлайном 100 мс:", errors.Is(err, context.DeadlineExceeded),
		"| отменённых ожиданий:", pool.Stat().CanceledAcquireCount())
	c1.Release()
	c2.Release()

	// Восемь запросов по 50 мс на двух соединениях. По закону
	// Литтла пул пропустит 2 / 0,05 с = 40 запросов в секунду;
	// остальные ждут в очереди, и их задержка растёт.
	before := pool.Stat().EmptyAcquireCount()
	start := make(chan struct{})
	var wg sync.WaitGroup
	var failed atomic.Int32
	for range 8 {
		wg.Go(func() {
			<-start
			if _, err := pool.Exec(ctx, `SELECT pg_sleep(0.05)`); err != nil {
				failed.Add(1)
			}
		})
	}
	close(start)
	wg.Wait()
	st := pool.Stat()
	fmt.Println("8 запросов на 2 соединениях: ошибок", failed.Load(),
		"| ждали соединение:", st.EmptyAcquireCount() > before, "| открыто:", st.TotalConns())
}

// Три способа оборвать долгий запрос. Пул на одно соединение:
// по `NewConnsCount` видно, пережило ли его соединение.
func timeouts(ctx context.Context, dsn string) {
	try := func(title string, setup func(*pgxpool.Config), run func(context.Context, *pgxpool.Pool) error) {
		cfg, err := pgxpool.ParseConfig(dsn)
		check(err)
		cfg.MaxConns = 1
		if setup != nil {
			setup(cfg)
		}
		pool, err := pgxpool.NewWithConfig(ctx, cfg)
		check(err)
		defer pool.Close()
		exec(ctx, pool, `SELECT 1`)
		err = run(ctx, pool)
		var pgErr *pgconn.PgError
		code := "-"
		if errors.As(err, &pgErr) {
			code = pgErr.Code
		}
		exec(ctx, pool, `SELECT 1`)
		fmt.Printf("%s: DeadlineExceeded=%v SQLSTATE=%s новое соединение=%v\n",
			title, errors.Is(err, context.DeadlineExceeded), code, pool.Stat().NewConnsCount() > 1)
	}
	withDeadline := func(ctx context.Context, pool *pgxpool.Pool) error {
		short, cancel := context.WithTimeout(ctx, 100*time.Millisecond)
		defer cancel()
		_, err := pool.Exec(short, `SELECT pg_sleep(5)`)
		return err
	}

	// Контекст по умолчанию: pgx ставит сокету дедлайн, рвёт
	// соединение и шлёт серверу CancelRequest. Пул откроет новое.
	try("контекст", nil, withDeadline)

	// `statement_timeout` обрывает запрос на сервере — ошибка
	// 57014, соединение остаётся в пуле. `SET LOCAL` действует
	// до конца транзакции и не протекает в чужие запросы.
	try("statement_timeout", nil, func(ctx context.Context, pool *pgxpool.Pool) error {
		return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, `SET LOCAL statement_timeout = '100ms'`); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `SELECT pg_sleep(5)`)
			return err
		})
	})

	// С `CancelRequestContextWatcherHandler` отмена контекста
	// сначала просит сервер отменить запрос — соединение живёт,
	// но ошибка — та же 57014, а не `DeadlineExceeded`.
	try("контекст + CancelRequest", func(cfg *pgxpool.Config) {
		cfg.ConnConfig.BuildContextWatcherHandler = func(c *pgconn.PgConn) ctxwatch.Handler {
			return &pgconn.CancelRequestContextWatcherHandler{Conn: c, DeadlineDelay: 2 * time.Second}
		}
	}, withDeadline)
}

type queryCounter struct{ n atomic.Int64 }

func (c *queryCounter) TraceQueryStart(ctx context.Context, _ *pgx.Conn, _ pgx.TraceQueryStartData) context.Context {
	c.n.Add(1)
	return ctx
}

func (c *queryCounter) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

type node struct {
	Type      string  `json:"Node Type"`
	Index     string  `json:"Index Name"`
	Direction string  `json:"Scan Direction"`
	Rows      float64 `json:"Actual Rows"`
	Plans     []node  `json:"Plans"`
}

// explain печатает цепочку узлов плана и сколько строк отдал
// самый нижний — тот, что читает индекс или таблицу.
func explain(ctx context.Context, pool *pgxpool.Pool, title, sql string, args ...any) {
	var out []struct{ Plan node }
	var raw []byte
	check(pool.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+sql, args...).Scan(&raw))
	check(json.Unmarshal(raw, &out))
	var chain []string
	n := out[0].Plan
	for {
		s := n.Type
		if n.Direction == "Backward" {
			s += " Backward"
		}
		if n.Index != "" {
			s += " using " + n.Index
		}
		chain = append(chain, s)
		if len(n.Plans) == 0 {
			break
		}
		n = n.Plans[0]
	}
	fmt.Printf("%s: %s, строк из индекса: %.0f\n", title, strings.Join(chain, " → "), n.Rows)
}

func exec(ctx context.Context, pool *pgxpool.Pool, sql string) {
	_, err := pool.Exec(ctx, sql)
	check(err)
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
