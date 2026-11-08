// `pgx` — драйвер PostgreSQL с собственным API: пул
// `pgxpool`, типы PostgreSQL (`jsonb`, массивы) без обёрток,
// `Batch`, `COPY`, `LISTEN/NOTIFY`. Пример идёт на настоящей
// базе профиля labs.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Product — строка таблицы; теги `db` читает
// `pgx.RowToStructByName`. `jsonb` маппится на структуру
// через `encoding/json`, `text[]` — на срез.
type Product struct {
	ID    int64    `db:"id"`
	SKU   string   `db:"sku"`
	Price int64    `db:"price"`
	Attrs Attrs    `db:"attrs"`
	Tags  []string `db:"tags"`
}

type Attrs struct {
	Color  string `json:"color"`
	Weight int    `json:"weight,omitempty"`
}

func main() {
	// Без профиля labs переменная пуста — сообщаем и выходим.
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		fmt.Println("нужен PostgreSQL из профиля labs: docker compose --profile labs up")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()

	// Пул настраивается до создания: `ParseConfig` + поля.
	// По умолчанию `MaxConns` = max(4, число CPU).
	cfg, err := pgxpool.ParseConfig(dsn)
	check(err)
	cfg.MaxConns = 4
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	check(err)
	defer pool.Close()

	_, err = pool.Exec(ctx, `CREATE TABLE products (
		id    bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
		sku   text NOT NULL UNIQUE,
		price bigint NOT NULL,
		attrs jsonb NOT NULL DEFAULT '{}',
		tags  text[] NOT NULL DEFAULT '{}')`)
	check(err)

	// Плейсхолдеры — `$1`, `$2`. `RETURNING` отдаёт
	// сгенерированный id в том же запросе — `LastInsertId` не нужен.
	var id int64
	err = pool.QueryRow(ctx, `INSERT INTO products (sku, price, attrs, tags)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		"mug", 450, Attrs{Color: "red", Weight: 300}, []string{"kitchen", "gift"}).Scan(&id)
	check(err)
	fmt.Println("вставлен id", id)

	// Нарушение ограничения — `*pgconn.PgError` с кодом
	// SQLSTATE: `23505` — unique_violation. Код стабилен,
	// текст сообщения — нет.
	_, err = pool.Exec(ctx, `INSERT INTO products (sku, price) VALUES ($1, $2)`, "mug", 500)
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		fmt.Println("ошибка:", pgErr.Code, pgErr.ConstraintName)
	}

	// Upsert: `ON CONFLICT ... DO UPDATE`; `EXCLUDED` — строка,
	// которую пытались вставить. `xmax = 0` — приём, чтобы
	// отличить вставку от обновления. Каждая попытка вставки,
	// даже неудачная, берёт номер из последовательности —
	// в id появляются дыры, и это нормально.
	upsert := `INSERT INTO products (sku, price) VALUES ($1, $2)
		ON CONFLICT (sku) DO UPDATE SET price = EXCLUDED.price
		RETURNING id, (xmax = 0) AS inserted`
	for _, p := range []struct {
		sku   string
		price int64
	}{{"mug", 500}, {"cup", 300}} {
		var inserted bool
		check(pool.QueryRow(ctx, upsert, p.sku, p.price).Scan(&id, &inserted))
		fmt.Println("upsert", p.sku, "id", id, "вставка:", inserted)
	}

	// `pgx.Batch` копит запросы и отправляет их за один
	// сетевой обмен; результаты читаются в том же порядке.
	batch := &pgx.Batch{}
	for i, sku := range []string{"plate", "bowl", "fork"} {
		batch.Queue(`INSERT INTO products (sku, price, attrs) VALUES ($1, $2, $3)`,
			sku, 100*(i+1), Attrs{Color: []string{"white", "red", "steel"}[i]})
	}
	check(pool.SendBatch(ctx, batch).Close())

	// `COPY FROM STDIN` — самый быстрый путь для больших
	// вставок: строки идут потоком, без разбора SQL на каждую.
	n, err := pool.CopyFrom(ctx, pgx.Identifier{"products"}, []string{"sku", "price"},
		pgx.CopyFromRows([][]any{{"spoon", 50}, {"knife", 70}}))
	check(err)
	fmt.Println("COPY строк:", n)

	// Срез уходит как массив PostgreSQL: `= ANY($1)` вместо
	// `IN (...)` с переменным числом плейсхолдеров. `@>` —
	// «jsonb содержит». `CollectRows` закрывает `rows` сам,
	// а ошибку `Query` pgx дублирует в `rows.Err()` — её
	// вернёт `CollectRows`.
	rows, _ := pool.Query(ctx, `SELECT id, sku, price, attrs, tags FROM products
		WHERE sku = ANY($1) OR attrs @> $2 ORDER BY id`,
		[]string{"mug", "spoon"}, Attrs{Color: "red"})
	products, err := pgx.CollectRows(rows, pgx.RowToStructByName[Product])
	check(err)
	for _, p := range products {
		fmt.Printf("%d %s %d %+v %v\n", p.ID, p.SKU, p.Price, p.Attrs, p.Tags)
	}

	// `LISTEN` привязан к соединению, поэтому слушателю нужно
	// своё: `Acquire` забирает его из пула до `Release`.
	listener, err := pool.Acquire(ctx)
	check(err)
	defer listener.Release()
	_, err = listener.Exec(ctx, `LISTEN product_events`)
	check(err)
	_, err = pool.Exec(ctx, `SELECT pg_notify('product_events', $1)`, "price changed: mug")
	check(err)
	note, err := listener.Conn().WaitForNotification(ctx)
	check(err)
	fmt.Println("уведомление:", note.Channel, note.Payload)

	// Advisory lock — блокировка по числу, а не по строке.
	// `pg_try_advisory_xact_lock` не ждёт и отпускается
	// концом транзакции: так выбирают единственного воркера.
	tx, err := pool.Begin(ctx)
	check(err)
	defer tx.Rollback(ctx)
	fmt.Println("воркер 1 взял замок:", tryLock(ctx, tx))
	fmt.Println("воркер 2 взял замок:", tryLock(ctx, pool))
	check(tx.Commit(ctx))
	fmt.Println("после commit воркер 2:", tryLock(ctx, pool))
}

func tryLock(ctx context.Context, q interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}) (ok bool) {
	check(q.QueryRow(ctx, `SELECT pg_try_advisory_xact_lock($1)`, int64(42)).Scan(&ok))
	return ok
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
