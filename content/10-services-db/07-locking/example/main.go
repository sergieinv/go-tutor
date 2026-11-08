// Потерянное обновление на настоящем PostgreSQL и три
// способа его избежать: пессимистичная блокировка
// `FOR UPDATE`, оптимистичная — по версии строки, и
// атомарный `UPDATE`. Две транзакции — два соединения;
// их шаги идут по очереди из одной горутины, а там, где
// вторая должна ждать, мы убеждаемся, что она ждёт.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"math/rand/v2"
	"os"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	_ "github.com/jackc/pgx/v5/stdlib" // драйвер "pgx" для database/sql
)

var ctx = context.Background()

func main() {
	dsn := os.Getenv("POSTGRES_DSN")
	if dsn == "" {
		fmt.Println("нужен PostgreSQL из профиля labs: docker compose --profile labs up")
		return
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	exec(db, `CREATE TABLE stock (id int PRIMARY KEY, qty int NOT NULL CHECK (qty >= 0), version int NOT NULL);
		CREATE TABLE jobs (id int PRIMARY KEY);
		INSERT INTO stock VALUES (1, 10, 1);
		INSERT INTO jobs VALUES (1), (2), (3);`)

	// Потерянное обновление: A и B читают 10, A пишет
	// 10-3, B пишет 10-4. Ошибок нет, но списание A пропало.
	a, b := begin(db), begin(db)
	qa, qb := qty(a, false), qty(b, false)
	exec(a, `UPDATE stock SET qty = $1 WHERE id = 1`, qa-3)
	commit(a)
	exec(b, `UPDATE stock SET qty = $1 WHERE id = 1`, qb-4)
	commit(b)
	fmt.Println("без блокировки: остаток", qty(db, false), "вместо 3")

	// `FOR UPDATE` блокирует строку до конца транзакции:
	// B ждёт на своём SELECT, пока A не зафиксируется, и
	// читает уже новое значение. `lock_timeout` — страховка,
	// чтобы B не ждала вечно, если что-то пойдёт не так.
	exec(db, `UPDATE stock SET qty = 10 WHERE id = 1`)
	a, b = begin(db), begin(db)
	qa = qty(a, true)
	exec(b, `SET LOCAL lock_timeout = '5s'`)
	pidB := pid(b)
	got := make(chan int)
	go func() { got <- qty(b, true) }()
	waitForLock(db, pidB)
	fmt.Println("B ждёт блокировку строки")
	exec(a, `UPDATE stock SET qty = $1 WHERE id = 1`, qa-3)
	commit(a)
	qb = <-got
	exec(b, `UPDATE stock SET qty = $1 WHERE id = 1`, qb-4)
	commit(b)
	fmt.Println("FOR UPDATE: B прочитала", qb, "остаток", qty(db, false))

	// `NOWAIT` не ждёт, а сразу возвращает ошибку 55P03.
	a, b = begin(db), begin(db)
	qty(a, true)
	err = b.QueryRowContext(ctx, `SELECT qty FROM stock WHERE id = 1 FOR UPDATE NOWAIT`).Scan(new(int))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		fmt.Println("NOWAIT:", pgErr.Code)
	}
	a.Rollback()
	b.Rollback()

	// `SKIP LOCKED` пропускает занятые строки: два воркера
	// очереди берут разные задачи и не ждут друг друга.
	a, b = begin(db), begin(db)
	const next = `SELECT id FROM jobs ORDER BY id LIMIT 1 FOR UPDATE SKIP LOCKED`
	var ja, jb int
	scan(a.QueryRowContext(ctx, next), &ja)
	scan(b.QueryRowContext(ctx, next), &jb)
	fmt.Println("SKIP LOCKED: A взяла задачу", ja, "B —", jb)
	a.Rollback()
	b.Rollback()

	// Оптимистичная блокировка: читаем версию без
	// блокировок, пишем с условием на неё. 0 строк —
	// кто-то успел раньше: конфликт, перечитать и повторить.
	exec(db, `UPDATE stock SET qty = 10, version = 1 WHERE id = 1`)
	errB := optimistic(db, 4, func() {
		// A успевает целиком между чтением и записью B.
		fmt.Println("версия: A списала 3:", optimistic(db, 3, nil))
	})
	fmt.Println("версия: B списала 4:", errB)
	fmt.Println("версия: B повторила:", optimistic(db, 4, nil), "остаток", qty(db, false))

	// В жизни конфликты случаются сами. Повторяем
	// всю операцию: перечитать и записать, — с ограничением
	// попыток и случайной паузой.
	exec(db, `UPDATE stock SET qty = 10 WHERE id = 1`)
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := range errs {
		wg.Go(func() {
			errs[i] = retry(10, func() error { return optimistic(db, 1, nil) })
		})
	}
	wg.Wait()
	fmt.Println("10 параллельных списаний:", errors.Join(errs...), "остаток", qty(db, false))

	// Атомарный `UPDATE`: проверка и изменение — один
	// запрос, база сама сериализует его на строке.
	exec(db, `UPDATE stock SET qty = 5 WHERE id = 1`)
	for range 2 {
		res, err := db.ExecContext(ctx, `UPDATE stock SET qty = qty - $1 WHERE id = 1 AND qty >= $1`, 3)
		if err != nil {
			log.Fatal(err)
		}
		n, _ := res.RowsAffected()
		fmt.Println("атомарно -3: строк", n, "остаток", qty(db, false))
	}
}

var errConflict = errors.New("конфликт версий")

// optimistic списывает n по версии строки. between
// выполняется между чтением и записью — так пример
// показывает конфликт без гонки.
func optimistic(db *sql.DB, n int, between func()) error {
	var q, ver int
	scan(db.QueryRowContext(ctx, `SELECT qty, version FROM stock WHERE id = 1`), &q, &ver)
	if between != nil {
		between()
	}
	res, err := db.ExecContext(ctx, `UPDATE stock SET qty = $1, version = version + 1 WHERE id = 1 AND version = $2`, q-n, ver)
	if err != nil {
		return err
	}
	if rows, _ := res.RowsAffected(); rows == 0 {
		return errConflict
	}
	return nil
}

// retry повторяет fn при конфликте: пауза растёт вдвое
// и выбирается случайно в этих пределах (джиттер), чтобы
// соперники не сталкивались снова в тот же момент.
func retry(attempts int, fn func() error) error {
	for i := range attempts {
		err := fn()
		if !errors.Is(err, errConflict) {
			return err
		}
		time.Sleep(rand.N(min(time.Millisecond<<i, 20*time.Millisecond)))
	}
	return errConflict
}

// waitForLock ждёт, пока сеанс pid встанет в ожидание
// блокировки: так мы знаем, что B действительно ждёт.
func waitForLock(db *sql.DB, pid int) {
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		var wait sql.NullString
		scan(db.QueryRowContext(ctx, `SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1`, pid), &wait)
		if wait.String == "Lock" {
			return
		}
	}
	log.Fatal("B не встала в ожидание блокировки")
}

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

// qty читает остаток; forUpdate — с блокировкой строки.
func qty(q querier, forUpdate bool) int {
	query := `SELECT qty FROM stock WHERE id = 1`
	if forUpdate {
		query = `SELECT qty FROM stock WHERE id = 1 FOR UPDATE`
	}
	var n int
	scan(q.QueryRowContext(ctx, query), &n)
	return n
}

func pid(q querier) int {
	var n int
	scan(q.QueryRowContext(ctx, `SELECT pg_backend_pid()`), &n)
	return n
}

func begin(db *sql.DB) *sql.Tx {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		log.Fatal(err)
	}
	return tx
}

func commit(tx *sql.Tx) {
	if err := tx.Commit(); err != nil {
		log.Fatal(err)
	}
}

func exec(q querier, query string, args ...any) {
	if _, err := q.ExecContext(ctx, query, args...); err != nil {
		log.Fatal(err)
	}
}

func scan(row *sql.Row, dest ...any) {
	if err := row.Scan(dest...); err != nil {
		log.Fatal(err)
	}
}
