// Индексы и планы запросов на настоящем PostgreSQL. Пример
// печатает только устойчивую часть плана — типы узлов, имена
// индексов и фактическое число строк: стоимости и время
// меняются от запуска к запуску.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
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
	conn, err := pgx.Connect(ctx, dsn)
	check(err)
	defer conn.Close(ctx)
	// Параллельные планы делят строки между воркерами, и их
	// число зависит от нагрузки сервера — отключаем.
	exec(ctx, conn, `SET max_parallel_workers_per_gather = 0`)

	// 100 000 заказов: 1000 клиентов, 1% — в статусе `new`,
	// время растёт вместе с id. Данные детерминированные.
	exec(ctx, conn, `CREATE TABLE orders (
		id          bigint PRIMARY KEY,
		customer_id int NOT NULL,
		status      text NOT NULL,
		email       text NOT NULL,
		total       int NOT NULL,
		attrs       jsonb NOT NULL,
		created_at  timestamptz NOT NULL);
	INSERT INTO orders
	SELECT g, g % 1000,
		CASE WHEN g % 100 = 0 THEN 'new' ELSE 'done' END,
		'user' || g || '@example.com', g % 5000,
		jsonb_build_object('channel', CASE WHEN g % 1000 = 0 THEN 'partner' ELSE 'web' END),
		timestamptz '2026-01-01 00:00:00+00' + g * interval '1 minute'
	FROM generate_series(1, 100000) g;
	ANALYZE orders`)

	show := func(title, sql string, args ...any) {
		fmt.Println("==", title)
		explain(ctx, conn, sql, args...)
	}
	byCustomer := `SELECT id, total FROM orders WHERE customer_id = $1 ORDER BY created_at DESC LIMIT 5`
	show("без индекса", byCustomer, 42)

	exec(ctx, conn, `CREATE INDEX orders_customer_created ON orders (customer_id, created_at)`)
	show("составной индекс (customer_id, created_at)", byCustomer, 42)
	firstHour := `SELECT count(*) FROM orders WHERE created_at < $1`
	hour := time.Date(2026, 1, 1, 1, 0, 0, 0, time.UTC)
	show("только вторая колонка", firstHour, hour)
	exec(ctx, conn, `CREATE INDEX orders_status_created ON orders (status, created_at)`)
	show("вторая колонка, у первой два значения", firstHour, hour)
	exec(ctx, conn, `DROP INDEX orders_status_created`)

	exec(ctx, conn, `CREATE INDEX orders_email ON orders (email)`)
	show("LIKE по префиксу, обычный индекс", `SELECT id FROM orders WHERE email LIKE $1`, "user4242@%")
	exec(ctx, conn, `CREATE INDEX orders_email_pattern ON orders (email text_pattern_ops)`)
	show("LIKE по префиксу, text_pattern_ops", `SELECT id FROM orders WHERE email LIKE $1`, "user4242@%")

	exec(ctx, conn, `CREATE INDEX orders_new ON orders (created_at) WHERE status = 'new'`)
	show("частичный индекс", `SELECT id FROM orders WHERE status = 'new' AND created_at < $1`,
		time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))

	exec(ctx, conn, `CREATE INDEX orders_customer_incl ON orders (customer_id) INCLUDE (total)`)
	exec(ctx, conn, `VACUUM orders`)
	show("покрывающий индекс", `SELECT sum(total) FROM orders WHERE customer_id = $1`, 7)

	exec(ctx, conn, `CREATE INDEX orders_attrs ON orders USING gin (attrs)`)
	show("GIN по jsonb", `SELECT count(*) FROM orders WHERE attrs @> $1`, `{"channel": "partner"}`)

	exec(ctx, conn, `CREATE INDEX orders_created_brin ON orders USING brin (created_at)`)
	show("BRIN по времени", `SELECT count(*) FROM orders WHERE created_at BETWEEN $1 AND $2`,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 2, 0, 0, 0, 0, time.UTC))

	// pgx по умолчанию (`QueryExecModeCacheStatement`) готовит
	// запрос с параметрами на сервере и кеширует его на этом
	// соединении: повторный вызов — сразу `Execute`. Режим
	// можно передать первым аргументом запроса.
	fmt.Println("== кеш подготовленных выражений pgx")
	before := prepared(ctx, conn)
	for i := range 3 {
		var n int
		check(conn.QueryRow(ctx, `SELECT count(*) FROM orders WHERE customer_id = $1`, i).Scan(&n))
	}
	fmt.Println("3 вызова, подготовлено новых:", prepared(ctx, conn)-before)
	before = prepared(ctx, conn)
	var n int
	check(conn.QueryRow(ctx, `SELECT count(*) FROM orders WHERE total = $1`, pgx.QueryExecModeExec, 7).Scan(&n))
	fmt.Println("режим Exec, подготовлено новых:", prepared(ctx, conn)-before)
}

// prepared считает подготовленные выражения сессии. Сам этот
// запрос идёт простым протоколом и в кеш не попадает.
func prepared(ctx context.Context, conn *pgx.Conn) (n int) {
	check(conn.QueryRow(ctx, `SELECT count(*) FROM pg_prepared_statements`,
		pgx.QueryExecModeSimpleProtocol).Scan(&n))
	return n
}

// node — узел плана из `EXPLAIN (FORMAT JSON)`; берём только
// нужные поля, остальное `encoding/json` пропустит.
type node struct {
	Type       string  `json:"Node Type"`
	Relation   string  `json:"Relation Name"`
	Index      string  `json:"Index Name"`
	Direction  string  `json:"Scan Direction"`
	Searches   int     `json:"Index Searches"`
	ActualRows float64 `json:"Actual Rows"`
	Hit        int     `json:"Shared Hit Blocks"`
	Read       int     `json:"Shared Read Blocks"`
	Plans      []node  `json:"Plans"`
}

// explain выполняет запрос под `EXPLAIN (ANALYZE, FORMAT JSON)`
// с теми же параметрами и печатает дерево узлов.
func explain(ctx context.Context, conn *pgx.Conn, sql string, args ...any) {
	var out []struct{ Plan node }
	var raw []byte
	check(conn.QueryRow(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+sql, args...).Scan(&raw))
	check(json.Unmarshal(raw, &out))
	root := out[0].Plan
	printNode(root, 0)
	// Буферы корня включают детей: столько страниц по 8 КБ
	// прочитал весь запрос — из кеша или с диска.
	fmt.Println("страниц прочитано:", root.Hit+root.Read)
}

func printNode(n node, depth int) {
	line := strings.Repeat("  ", depth) + n.Type
	if n.Direction == "Backward" {
		line += " Backward"
	}
	if n.Index != "" {
		line += " using " + n.Index
	}
	if n.Relation != "" {
		line += " on " + n.Relation
	}
	line += fmt.Sprintf(" (строк: %.0f", n.ActualRows)
	if n.Searches > 0 {
		line += fmt.Sprintf(", спусков по индексу: %d", n.Searches)
	}
	fmt.Println(line + ")")
	for _, c := range n.Plans {
		printNode(c, depth+1)
	}
}

func exec(ctx context.Context, conn *pgx.Conn, sql string) {
	_, err := conn.Exec(ctx, sql)
	check(err)
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
