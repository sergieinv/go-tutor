// Декларативное партиционирование PostgreSQL: таблица заказов,
// разбитая по месяцам. Пример печатает, в какие партиции легли
// строки и какие партиции читает план, — только имена и счётчики.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func main() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		fmt.Println("нужен PostgreSQL из профиля labs: docker compose --profile labs up")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cfg, err := pgx.ParseConfig(dsn)
	check(err)
	// Границы партиций по timestamptz зависят от часового пояса
	// сессии, если в литерале нет смещения. Пишем `+00` явно, а
	// пояс фиксируем, чтобы вывод не зависел от сервера.
	cfg.RuntimeParams["timezone"] = "UTC"
	conn, err := pgx.ConnectConfig(ctx, cfg)
	check(err)
	defer conn.Close(ctx)

	// Родительская таблица данных не хранит: она описывает схему
	// и ключ партиционирования. Строки живут в партициях.
	exec(ctx, conn, `CREATE TABLE orders (
		id          bigint NOT NULL,
		customer_id bigint NOT NULL,
		created_at  timestamptz NOT NULL,
		total       bigint NOT NULL
	) PARTITION BY RANGE (created_at)`)
	for m := 1; m <= 4; m++ {
		from := time.Date(2026, time.Month(m), 1, 0, 0, 0, 0, time.UTC)
		to := from.AddDate(0, 1, 0)
		exec(ctx, conn, fmt.Sprintf(`CREATE TABLE orders_%s PARTITION OF orders
			FOR VALUES FROM ('%s') TO ('%s')`, from.Format("2006_01"), lit(from), lit(to)))
	}
	// DEFAULT ловит всё, что не попало ни в один диапазон; без неё
	// вставка такой строки — ошибка.
	exec(ctx, conn, `CREATE TABLE orders_default PARTITION OF orders DEFAULT`)

	// Индекс на родителе создаётся на каждой партиции (и на
	// будущих — автоматически).
	exec(ctx, conn, `CREATE INDEX orders_customer ON orders (customer_id, created_at)`)

	// Заказ каждый час с января по апрель и один «из будущего».
	exec(ctx, conn, `INSERT INTO orders
	SELECT g, g % 500, timestamptz '2026-01-01 00:00:00+00' + (g - 1) * interval '1 hour', g % 1000
	FROM generate_series(1, 2880) g;
	INSERT INTO orders VALUES (100000, 1, '2027-01-01 00:00:00+00', 1);
	ANALYZE orders`)
	rows, _ := conn.Query(ctx, `SELECT tableoid::regclass::text, count(*) FROM orders GROUP BY 1 ORDER BY 1`)
	var parts []string
	var part string
	var n int
	_, err = pgx.ForEachRow(rows, []any{&part, &n}, func() error {
		parts = append(parts, fmt.Sprintf("%s=%d", part, n))
		return nil
	})
	check(err)
	fmt.Println("строки по партициям:", strings.Join(parts, " "))
	var indexes int
	check(conn.QueryRow(ctx, `SELECT count(*) FROM pg_indexes WHERE tablename LIKE 'orders\_%'`).Scan(&indexes))
	fmt.Println("индексов на партициях:", indexes)

	// Partition pruning: условие на ключ — и план читает одну
	// партицию из пяти.
	feb := `SELECT count(*) FROM orders WHERE created_at >= $1 AND created_at < $2`
	scanned(ctx, conn, "10–20 февраля", feb,
		time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC), time.Date(2026, 2, 20, 0, 0, 0, 0, time.UTC))
	// Без ключа партиционирования — все партиции.
	scanned(ctx, conn, "только покупатель", `SELECT count(*) FROM orders WHERE customer_id = $1`, 42)
	// Функция над ключом прячет его от планировщика.
	scanned(ctx, conn, "date_trunc над ключом",
		`SELECT count(*) FROM orders WHERE date_trunc('month', created_at) = $1`,
		time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC))

	// Обобщённый план подготовленного выражения не знает значения
	// параметра — отсечение происходит при выполнении, а план
	// честно сообщает, сколько партиций выброшено.
	exec(ctx, conn, `SET plan_cache_mode = force_generic_plan`)
	exec(ctx, conn, `PREPARE by_day(timestamptz) AS
		SELECT count(*) FROM orders WHERE created_at >= $1 AND created_at < $1 + interval '1 day'`)
	scanned(ctx, conn, "обобщённый план", `EXECUTE by_day('2026-03-15 00:00:00+00')`)
	exec(ctx, conn, `RESET plan_cache_mode`)

	// Уникальность проверяется внутри каждой партиции, поэтому
	// уникальный ключ обязан включать ключ партиционирования.
	_, err = conn.Exec(ctx, `ALTER TABLE orders ADD PRIMARY KEY (id)`)
	fmt.Println("PRIMARY KEY (id):", sqlstate(err))
	_, err = conn.Exec(ctx, `ALTER TABLE orders ADD PRIMARY KEY (id, created_at)`)
	fmt.Println("PRIMARY KEY (id, created_at):", sqlstate(err))

	// Удаление старых данных: отсоединить партицию и удалить её
	// таблицу — мгновенно и без мёртвых строк, в отличие от
	// `DELETE … WHERE created_at < …` по миллионам строк.
	var before, after int
	check(conn.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&before))
	exec(ctx, conn, `ALTER TABLE orders DETACH PARTITION orders_2026_01`)
	exec(ctx, conn, `DROP TABLE orders_2026_01`)
	check(conn.QueryRow(ctx, `SELECT count(*) FROM orders`).Scan(&after))
	fmt.Println("строк до и после отсоединения января:", before, after)

	// Hash-партиционирование раскладывает покупателей по
	// остатку от хеша — равномерно, но без диапазонов.
	exec(ctx, conn, `CREATE TABLE carts (customer_id bigint NOT NULL, items int NOT NULL)
		PARTITION BY HASH (customer_id)`)
	for r := range 4 {
		exec(ctx, conn, fmt.Sprintf(`CREATE TABLE carts_%d PARTITION OF carts
			FOR VALUES WITH (MODULUS 4, REMAINDER %d)`, r, r))
	}
	exec(ctx, conn, `INSERT INTO carts SELECT g, 1 FROM generate_series(1, 1000) g`)
	rows, _ = conn.Query(ctx, `SELECT tableoid::regclass::text, count(*) FROM carts GROUP BY 1 ORDER BY 1`)
	parts = nil
	_, err = pgx.ForEachRow(rows, []any{&part, &n}, func() error {
		parts = append(parts, fmt.Sprintf("%s=%d", part, n))
		return nil
	})
	check(err)
	fmt.Println("корзины по hash-партициям:", strings.Join(parts, " "))
	scanned(ctx, conn, "корзина покупателя", `SELECT items FROM carts WHERE customer_id = $1`, 42)
}

// lit — литерал timestamptz с явным смещением UTC.
func lit(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05") + "+00" }

type node struct {
	Relation string `json:"Relation Name"`
	Removed  int    `json:"Subplans Removed"`
	Plans    []node `json:"Plans"`
}

// scanned печатает партиции, которые читает план, и сколько
// подпланов отброшено при выполнении.
func scanned(ctx context.Context, conn *pgx.Conn, title, sql string, args ...any) {
	var out []struct{ Plan node }
	var raw []byte
	check(conn.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+sql, args...).Scan(&raw))
	check(json.Unmarshal(raw, &out))
	var rels []string
	removed := 0
	var walk func(n node)
	walk = func(n node) {
		if n.Relation != "" && !slices.Contains(rels, n.Relation) {
			rels = append(rels, n.Relation)
		}
		removed += n.Removed
		for _, c := range n.Plans {
			walk(c)
		}
	}
	walk(out[0].Plan)
	slices.Sort(rels)
	line := fmt.Sprintf("%s: %s", title, strings.Join(rels, ", "))
	if removed > 0 {
		line += fmt.Sprintf(" (отброшено при выполнении: %d)", removed)
	}
	fmt.Println(line)
}

func sqlstate(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code + " " + pgErr.Message
	}
	if err != nil {
		return err.Error()
	}
	return "ок"
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
