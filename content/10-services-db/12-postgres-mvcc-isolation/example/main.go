// Аномалии изоляции на настоящем PostgreSQL. Две транзакции
// идут на двух выделенных соединениях `a` и `b`, а шаги
// выполняются по очереди из одной горутины — порядок задан
// кодом, а не планировщиком и не `time.Sleep`.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
	cfg, err := pgxpool.ParseConfig(dsn)
	check(err)
	cfg.ConnConfig.OnNotice = onNotice // для VACUUM (VERBOSE), см. ниже
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	check(err)
	defer pool.Close()

	// `Acquire` забирает соединение из пула до `Release`:
	// транзакция живёт на одном соединении. `lock_timeout` —
	// страховка, чтобы зависшая блокировка не съела лимит.
	a, b := acquire(ctx, pool), acquire(ctx, pool)
	defer a.Release()
	defer b.Release()
	exec(ctx, pool, `CREATE TABLE accounts (id int PRIMARY KEY, balance int NOT NULL);
		INSERT INTO accounts VALUES (1, 100)`)

	// MVCC: `UPDATE` не меняет строку на месте, а пишет новую
	// версию (`ctid` — её адрес). Снимок `a` видит старую
	// версию, и читатель не ждёт писателя.
	fmt.Println("== MVCC: две версии строки")
	ta := begin(ctx, a, pgx.RepeatableRead)
	fmt.Println("a видит:", row(ctx, ta))
	exec(ctx, b, `UPDATE accounts SET balance = 90 WHERE id = 1`)
	fmt.Println("b после UPDATE:", row(ctx, b))
	fmt.Println("a всё ещё видит:", row(ctx, ta))
	check(ta.Commit(ctx))

	// Read Committed (по умолчанию) берёт новый снимок на
	// каждый запрос: два чтения в одной транзакции расходятся.
	fmt.Println("== Read Committed: неповторяемое чтение")
	exec(ctx, pool, `UPDATE accounts SET balance = 100`)
	ta = begin(ctx, a, pgx.ReadCommitted)
	fmt.Println("a читает:", balance(ctx, ta))
	exec(ctx, b, `UPDATE accounts SET balance = 50`)
	fmt.Println("a читает снова:", balance(ctx, ta))
	check(ta.Commit(ctx))

	// Repeatable Read — один снимок на транзакцию. Если строку,
	// которую `a` собирается менять, изменила и зафиксировала
	// другая транзакция, `a` получает `40001`. Пока `b` не
	// завершилась, `UPDATE` в `a` ждёт блокировку строки —
	// запускаем его в горутине и дожидаемся, что он ждёт.
	fmt.Println("== Repeatable Read: конфликт обновления")
	exec(ctx, pool, `UPDATE accounts SET balance = 100`)
	ta = begin(ctx, a, pgx.RepeatableRead)
	read := balance(ctx, ta)
	fmt.Println("a читает:", read)
	tb := begin(ctx, b, pgx.ReadCommitted)
	exec(ctx, tb, `UPDATE accounts SET balance = balance - 30`)
	done := make(chan error)
	go func() {
		_, err := ta.Exec(ctx, `UPDATE accounts SET balance = $1`, read-50)
		done <- err
	}()
	waitLock(ctx, pool, a.Conn().PgConn().PID())
	fmt.Println("a ждёт блокировку строки")
	check(tb.Commit(ctx))
	fmt.Println("a получил:", sqlstate(<-done))
	check(ta.Rollback(ctx))
	// Лекарство — повторить транзакцию целиком: новый снимок
	// увидит списание `b`.
	ta = begin(ctx, a, pgx.RepeatableRead)
	read = balance(ctx, ta)
	exec(ctx, ta, `UPDATE accounts SET balance = $1`, read-50)
	check(ta.Commit(ctx))
	fmt.Println("повтор: прочитал", read, "записал", read-50)

	// Write skew: каждая транзакция проверяет, что дежурных
	// двое, и снимает с дежурства себя. Строки разные —
	// Repeatable Read конфликта не видит. Serializable (SSI)
	// отслеживает зависимости чтение→запись и обрывает одну.
	exec(ctx, pool, `CREATE TABLE doctors (name text PRIMARY KEY, on_call bool NOT NULL)`)
	for _, iso := range []pgx.TxIsoLevel{pgx.RepeatableRead, pgx.Serializable} {
		fmt.Println("== write skew,", iso)
		exec(ctx, pool, `DELETE FROM doctors; INSERT INTO doctors VALUES ('alice', true), ('bob', true)`)
		ta, tb := begin(ctx, a, iso), begin(ctx, b, iso)
		fmt.Println("a видит дежурных:", onCall(ctx, ta), "b видит:", onCall(ctx, tb))
		fmt.Println("a снимает alice:", sqlstate(execErr(ctx, ta, `UPDATE doctors SET on_call = false WHERE name = 'alice'`)))
		fmt.Println("b снимает bob:", sqlstate(execErr(ctx, tb, `UPDATE doctors SET on_call = false WHERE name = 'bob'`)))
		fmt.Println("commit a:", sqlstate(ta.Commit(ctx)))
		fmt.Println("commit b:", sqlstate(tb.Commit(ctx)))
		fmt.Println("дежурных осталось:", onCall(ctx, pool))
	}

	// Мёртвые версии убирает `VACUUM` (обычно autovacuum —
	// здесь он выключен, чтобы размеры не плавали). Он не
	// трогает версии, которые ещё может видеть чей-то снимок:
	// одна долгая транзакция — и таблица пухнет.
	fmt.Println("== VACUUM и долгая транзакция")
	exec(ctx, pool, `CREATE TABLE items (id int PRIMARY KEY, qty int NOT NULL) WITH (autovacuum_enabled = false);
		INSERT INTO items SELECT g, 0 FROM generate_series(1, 10000) g`)
	fmt.Println("страниц после вставки:", pages(ctx, pool))
	exec(ctx, pool, `UPDATE items SET qty = qty + 1`)
	fmt.Println("после UPDATE всех строк:", pages(ctx, pool))
	vacuum(ctx, pool, true)
	ta = begin(ctx, a, pgx.RepeatableRead)
	exec(ctx, ta, `SELECT count(*) FROM items`) // снимок взят
	exec(ctx, pool, `UPDATE items SET qty = qty + 1`)
	fmt.Println("VACUUM освободил место, UPDATE его занял:", pages(ctx, pool))
	fmt.Println("VACUUM при открытом снимке, не удалить:", vacuum(ctx, pool, false))
	exec(ctx, pool, `UPDATE items SET qty = qty + 1`)
	fmt.Println("UPDATE пишет в новые страницы:", pages(ctx, pool))
	check(ta.Commit(ctx))
	vacuum(ctx, pool, true)
	exec(ctx, pool, `VACUUM FULL items`)
	fmt.Println("после COMMIT, VACUUM и VACUUM FULL:", pages(ctx, pool))
}

// vacuum запускает `VACUUM (VERBOSE)` и возвращает, сколько
// мёртвых версий нельзя удалить: сервер пишет это в сообщении
// INFO. Горизонт очистки общий для сервера — снимок могут на
// миг держать и чужие соединения, — поэтому с clean=true
// повторяем, пока не удалится всё. Это ожидание факта, а не
// задержка наугад.
func vacuum(ctx context.Context, pool *pgxpool.Pool, clean bool) int {
	for {
		notRemovable = -1
		exec(ctx, pool, `VACUUM (VERBOSE) items`)
		if notRemovable < 0 {
			log.Fatal("нет статистики VACUUM")
		}
		if !clean || notRemovable == 0 {
			return notRemovable
		}
		select {
		case <-ctx.Done():
			log.Fatal("VACUUM не дождался горизонта")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

var (
	notRemovable   = -1
	notRemovableRe = regexp.MustCompile(`(\d+) are dead but not yet removable`)
)

// onNotice вызывается pgx в той же горутине, что выполняет
// запрос, поэтому без мьютекса.
func onNotice(_ *pgconn.PgConn, n *pgconn.Notice) {
	if m := notRemovableRe.FindStringSubmatch(n.Message); m != nil {
		notRemovable, _ = strconv.Atoi(m[1])
	}
}

// pages — размер таблицы в страницах по 8 КБ.
func pages(ctx context.Context, q querier) (n int) {
	check(q.QueryRow(ctx, `SELECT pg_relation_size('items') / current_setting('block_size')::int`).Scan(&n))
	return n
}

// querier — общее у пула, соединения и транзакции.
type querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

func acquire(ctx context.Context, pool *pgxpool.Pool) *pgxpool.Conn {
	c, err := pool.Acquire(ctx)
	check(err)
	exec(ctx, c, `SET lock_timeout = '5s'`)
	return c
}

func begin(ctx context.Context, c *pgxpool.Conn, iso pgx.TxIsoLevel) pgx.Tx {
	tx, err := c.BeginTx(ctx, pgx.TxOptions{IsoLevel: iso})
	check(err)
	return tx
}

// waitLock ждёт, пока серверный процесс pid встанет в очередь
// за блокировкой. Опрос — не способ упорядочить шаги: мы
// проверяем факт ожидания, а не надеемся на задержку.
func waitLock(ctx context.Context, pool *pgxpool.Pool, pid uint32) {
	for {
		var waiting bool
		check(pool.QueryRow(ctx, `SELECT coalesce(wait_event_type = 'Lock', false)
			FROM pg_stat_activity WHERE pid = $1`, pid).Scan(&waiting))
		if waiting {
			return
		}
		select {
		case <-ctx.Done():
			log.Fatal("не дождались блокировки")
		case <-time.After(5 * time.Millisecond):
		}
	}
}

func row(ctx context.Context, q querier) string {
	var ctid string
	var bal int
	check(q.QueryRow(ctx, `SELECT ctid::text, balance FROM accounts WHERE id = 1`).Scan(&ctid, &bal))
	return fmt.Sprintf("версия %s, баланс %d", ctid, bal)
}

func balance(ctx context.Context, q querier) (n int) {
	check(q.QueryRow(ctx, `SELECT balance FROM accounts WHERE id = 1`).Scan(&n))
	return n
}

func onCall(ctx context.Context, q querier) (n int) {
	check(q.QueryRow(ctx, `SELECT count(*) FROM doctors WHERE on_call`).Scan(&n))
	return n
}

// sqlstate печатает код ошибки PostgreSQL — по нему, а не по
// тексту, программа решает, повторять ли транзакцию.
func sqlstate(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &pgErr):
		return pgErr.Code + " " + pgErr.Message
	default:
		return err.Error()
	}
}

func execErr(ctx context.Context, q querier, sql string, args ...any) error {
	_, err := q.Exec(ctx, sql, args...)
	return err
}

func exec(ctx context.Context, q querier, sql string, args ...any) {
	check(execErr(ctx, q, sql, args...))
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
