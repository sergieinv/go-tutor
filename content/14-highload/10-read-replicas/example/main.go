// Запись на основной сервер, чтение с потоковой реплики. Реплика
// labs применяет транзакции с задержкой 200 мс, поэтому только что
// записанное на ней видно не сразу. Пример ждёт «доехавшую» запись
// по LSN — позиции в журнале WAL — и печатает только то, что от
// точного времени не зависит.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	dsn, rdsn := os.Getenv("POSTGRES_DSN"), os.Getenv("POSTGRES_REPLICA_DSN")
	if dsn == "" {
		fmt.Println("нужен PostgreSQL из профиля labs: docker compose --profile labs up")
		return
	}
	if rdsn == "" {
		fmt.Println("нужна реплика postgres-replica из профиля labs: docker compose --profile labs up")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	primary, err := pgxpool.New(ctx, dsn)
	check(err)
	defer primary.Close()
	replica, err := pgxpool.New(ctx, rdsn)
	check(err)
	defer replica.Close()

	// Реплика — сервер в режиме восстановления: она проигрывает
	// WAL основного и отказывает в записи (SQLSTATE 25006).
	var recovery bool
	check(replica.QueryRow(ctx, `SELECT pg_is_in_recovery()`).Scan(&recovery))
	_, err = replica.Exec(ctx, `CREATE TABLE t (id int)`)
	fmt.Println("реплика в восстановлении:", recovery, "| запись на реплику:", sqlstate(err))
	// `pg_stat_replication` на основном — по строке на реплику.
	// Адреса и задержки видит только роль с `pg_read_all_stats`.
	var standbys int
	check(primary.QueryRow(ctx, `SELECT count(*) FROM pg_stat_replication`).Scan(&standbys))
	fmt.Println("реплик в pg_stat_replication:", standbys)

	// Схема и старые заказы. DDL идёт через WAL так же, как
	// данные, — ждём, пока всё доедет до реплики.
	_, err = primary.Exec(ctx, `CREATE TABLE orders (
		id bigserial PRIMARY KEY, customer_id bigint NOT NULL, total bigint NOT NULL);
	INSERT INTO orders (customer_id, total) VALUES (7, 100), (7, 250)`)
	check(err)
	waitReplay(ctx, replica, currentLSN(ctx, primary))
	r := &router{primary: primary, replica: replica}

	// Новый заказ и токен: LSN основного **после** COMMIT.
	// Реплика, проигравшая WAL до этой позиции, видит заказ.
	// Сразу после записи реплика ещё не догнала токен, и роутер
	// идёт на основной.
	token, onReplica, n, from := writeAndRead(ctx, primary, r)
	fmt.Println("сразу после записи на реплике заказов:", onReplica)
	fmt.Printf("роутер с токеном: %d заказа, источник — %s\n", n, from)

	// Ждём реплику циклом с пределом (не `time.Sleep` на глаз).
	waitReplay(ctx, replica, token)
	n, from = r.history(ctx, 7, token)
	fmt.Printf("реплика догнала токен: %d заказа, источник — %s\n", n, from)

	// Задержка по времени: сейчас минус время коммита последней
	// проигранной транзакции. Реплика labs применяет коммит не
	// раньше чем через 200 мс (`recovery_min_apply_delay`).
	var lagged bool
	check(replica.QueryRow(ctx,
		`SELECT now() - pg_last_xact_replay_timestamp() >= interval '200 ms'`).Scan(&lagged))
	fmt.Println("задержка применения не меньше 200 мс:", lagged)
}

// router читает историю с реплики, если она проиграла WAL до
// токена клиента, иначе — с основного (read-your-writes).
type router struct{ primary, replica *pgxpool.Pool }

func (r *router) history(ctx context.Context, customer int64, token string) (int, string) {
	pool, from := r.primary, "основной"
	var caught bool
	err := r.replica.QueryRow(ctx, `SELECT pg_last_wal_replay_lsn() >= $1::pg_lsn`, token).Scan(&caught)
	if err == nil && caught {
		pool, from = r.replica, "реплика"
	}
	var n int
	check(pool.QueryRow(ctx, `SELECT count(*) FROM orders WHERE customer_id = $1`, customer).Scan(&n))
	return n, from
}

// writeAndRead вставляет заказ, берёт токен и сразу читает: прямо
// с реплики и через роутер. Коммит применяется на реплике не раньше
// чем через 200 мс, поэтому чтениям, уложившимся в 100 мс, можно
// верить; если машина замешкалась — удаляем заказ и повторяем.
func writeAndRead(ctx context.Context, primary *pgxpool.Pool, r *router) (token string, onReplica, n int, from string) {
	for range 5 {
		var id int64
		check(primary.QueryRow(ctx,
			`INSERT INTO orders (customer_id, total) VALUES (7, 300) RETURNING id`).Scan(&id))
		start := time.Now()
		token = currentLSN(ctx, primary)
		check(r.replica.QueryRow(ctx, `SELECT count(*) FROM orders WHERE customer_id = 7`).Scan(&onReplica))
		n, from = r.history(ctx, 7, token)
		if time.Since(start) < 100*time.Millisecond {
			return token, onReplica, n, from
		}
		_, err := primary.Exec(ctx, `DELETE FROM orders WHERE id = $1`, id)
		check(err)
	}
	log.Fatal("реплика отвечает слишком медленно")
	return "", 0, 0, ""
}

// currentLSN — позиция записи WAL основного. Вызывается вне
// транзакции записи: LSN, взятый внутри неё, меньше позиции
// её COMMIT.
func currentLSN(ctx context.Context, primary *pgxpool.Pool) string {
	var lsn string
	check(primary.QueryRow(ctx, `SELECT pg_current_wal_lsn()::text`).Scan(&lsn))
	return lsn
}

// waitReplay ждёт, пока реплика проиграет WAL до lsn: опрос раз в
// 10 мс, предел — 5 секунд.
func waitReplay(ctx context.Context, replica *pgxpool.Pool, lsn string) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for {
		var ok bool
		err := replica.QueryRow(ctx, `SELECT pg_last_wal_replay_lsn() >= $1::pg_lsn`, lsn).Scan(&ok)
		if err == nil && ok {
			return
		}
		select {
		case <-ctx.Done():
			log.Fatal("реплика не догнала основной за 5 с")
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func sqlstate(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return fmt.Sprint(err)
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
