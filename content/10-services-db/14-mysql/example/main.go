// MySQL из Go — это `database/sql` и драйвер
// `github.com/go-sql-driver/mysql`. Пример идёт на настоящем
// MySQL 8.4 профиля labs: DSN приходит в `MYSQL_DSN`.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/go-sql-driver/mysql"
)

func main() {
	dsn := os.Getenv("MYSQL_DSN")
	if dsn == "" {
		fmt.Println("нужен MySQL из профиля labs: docker compose --profile labs up")
		return
	}
	db, err := open(dsn)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := run(context.Background(), db); err != nil {
		log.Fatal(err)
	}
}

// DSN — строка, но собирать её надо через `mysql.Config`:
// `ParseDSN` → правка полей → `NewConnector`. Без
// `ParseTime` столбцы `DATETIME` читаются как `[]byte`;
// `Loc` — пояс, в котором драйвер пишет и читает время;
// `Collation` — сравнение строк соединения. Таймауты: `Timeout`
// — на подключение, `ReadTimeout`/`WriteTimeout` — на сетевой
// ввод-вывод. `Params` драйвер выполняет как `SET` при
// каждом подключении: ждать блокировку 50 с по умолчанию —
// слишком долго.
func open(dsn string) (*sql.DB, error) {
	cfg, err := mysql.ParseDSN(dsn)
	if err != nil {
		return nil, err
	}
	cfg.ParseTime = true
	cfg.Loc = time.UTC
	cfg.Collation = "utf8mb4_0900_ai_ci"
	cfg.Timeout = 3 * time.Second
	cfg.ReadTimeout, cfg.WriteTimeout = 5*time.Second, 5*time.Second
	cfg.Params = map[string]string{"innodb_lock_wait_timeout": "3"}
	connector, err := mysql.NewConnector(cfg)
	if err != nil {
		return nil, err
	}
	return sql.OpenDB(connector), nil
}

func run(ctx context.Context, db *sql.DB) error {
	for _, q := range []string{
		`CREATE TABLE orders (id BIGINT AUTO_INCREMENT PRIMARY KEY, title VARCHAR(50) NOT NULL, created DATETIME NOT NULL)`,
		`CREATE TABLE stock (sku VARCHAR(20) PRIMARY KEY, qty INT NOT NULL)`,
		`CREATE TABLE legacy (name VARCHAR(20) CHARACTER SET utf8mb3)`,
		`CREATE TABLE slots (id INT PRIMARY KEY)`,
		`INSERT INTO slots VALUES (10), (20), (30)`,
		`CREATE TABLE accounts (id INT PRIMARY KEY, balance INT NOT NULL)`,
		`INSERT INTO accounts VALUES (1, 100), (2, 100)`,
		`CREATE TABLE audit (id INT AUTO_INCREMENT PRIMARY KEY, msg VARCHAR(50))`,
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			return err
		}
	}

	var iso string
	if err := db.QueryRowContext(ctx, `SELECT @@transaction_isolation`).Scan(&iso); err != nil {
		return err
	}
	fmt.Println("изоляция по умолчанию:", iso)

	// `RETURNING` в MySQL нет: id новой строки — `LastInsertId`.
	// Время драйвер переводит в `Loc` (UTC): момент тот же,
	// пояс `DATETIME` не хранит.
	msk := time.FixedZone("MSK", 3*60*60)
	res, err := db.ExecContext(ctx, `INSERT INTO orders (title, created) VALUES (?, ?)`,
		"кофе ☕ и пицца 🍕", time.Date(2026, 3, 1, 12, 0, 0, 0, msk))
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	var title string
	var created time.Time
	err = db.QueryRowContext(ctx, `SELECT title, created FROM orders WHERE id = ?`, id).Scan(&title, &created)
	if err != nil {
		return err
	}
	fmt.Println("заказ", id, title, created)

	// `utf8` в MySQL — это `utf8mb3`: до трёх байт на символ.
	// ☕ (U+2615) влезает, 🍕 (U+1F355, четыре байта) — нет:
	// в строгом режиме это ошибка 1366.
	for _, s := range []string{"кофе ☕", "пицца 🍕"} {
		_, err := db.ExecContext(ctx, `INSERT INTO legacy VALUES (?)`, s)
		fmt.Printf("utf8mb3 %q: %s\n", s, code(err))
	}

	// `ON CONFLICT` здесь пишется как `ON DUPLICATE KEY UPDATE`.
	// `RowsAffected`: 1 — вставка, 2 — обновление,
	// 0 — строка уже была такой же.
	for _, qty := range []int{5, 7, 7} {
		res, err := db.ExecContext(ctx, `INSERT INTO stock (sku, qty) VALUES (?, ?) AS new
			ON DUPLICATE KEY UPDATE qty = new.qty`, "cup", qty)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		fmt.Printf("upsert qty=%d: rows affected %d\n", qty, n)
	}

	if err := gapLock(ctx, db); err != nil {
		return err
	}
	return deadlock(ctx, db)
}

// Gap lock. В Repeatable Read `SELECT … FOR UPDATE` по
// диапазону блокирует не только найденные строки, но и
// промежутки между ними (next-key lock): вставка в диапазон
// ждёт. Две транзакции — на двух соединениях; ожидание второй
// видно в `performance_schema.data_locks`, без `time.Sleep`.
func gapLock(ctx context.Context, db *sql.DB) error {
	a, err := begin(ctx, db, sql.LevelRepeatableRead)
	if err != nil {
		return err
	}
	defer a.close()
	b, err := begin(ctx, db, sql.LevelDefault)
	if err != nil {
		return err
	}
	defer b.close()

	var n int
	err = a.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM slots WHERE id BETWEEN 15 AND 25 FOR UPDATE`).Scan(&n)
	if err != nil {
		return err
	}
	fmt.Println("A: FOR UPDATE id 15..25 — строк", n, "| блокировки A:", a.locks(ctx, db))

	done := b.execAsync(ctx, `INSERT INTO slots VALUES (17)`)
	fmt.Println("B: INSERT 17 ждёт", waiting(ctx, db, b))
	if err := a.tx.Commit(); err != nil {
		return err
	}
	fmt.Println("A: COMMIT → B:", code(<-done))
	if err := b.tx.Commit(); err != nil {
		return err
	}

	// В Read Committed промежутки не блокируются: фантом
	// возможен, зато вставка не ждёт.
	a2, err := begin(ctx, db, sql.LevelReadCommitted)
	if err != nil {
		return err
	}
	defer a2.close()
	err = a2.tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM slots WHERE id BETWEEN 15 AND 25 FOR UPDATE`).Scan(&n)
	if err != nil {
		return err
	}
	_, err = db.ExecContext(ctx, `INSERT INTO slots VALUES (18)`)
	fmt.Println("READ COMMITTED: блокировки A:", a2.locks(ctx, db), "| INSERT 18:", code(err))
	return a2.tx.Commit()
}

// Дедлок: A и B обновляют два счёта в разном порядке.
// InnoDB находит цикл сразу и откатывает «более лёгкую»
// транзакцию — ту, что изменила меньше строк (здесь B).
// Ошибка 1213 — сигнал повторить транзакцию целиком.
func deadlock(ctx context.Context, db *sql.DB) error {
	a, err := begin(ctx, db, sql.LevelDefault)
	if err != nil {
		return err
	}
	defer a.close()
	for _, q := range []string{
		`INSERT INTO audit (msg) VALUES ('A: перевод 1→2')`,
		`UPDATE accounts SET balance = balance - 10 WHERE id = 1`,
	} {
		if _, err := a.tx.ExecContext(ctx, q); err != nil {
			return err
		}
	}

	attempt := 0
	err = retry(ctx, db, 3, func(tx *sql.Tx) error {
		attempt++
		if _, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance - 20 WHERE id = 2`); err != nil {
			return err
		}
		var done <-chan error
		if attempt == 1 {
			// Первая попытка: A просит строку 2 и ждёт B,
			// затем B просит строку 1 — цикл.
			done = a.execAsync(ctx, `UPDATE accounts SET balance = balance + 10 WHERE id = 2`)
			fmt.Println("A: UPDATE id=2 ждёт", waiting(ctx, db, a))
		}
		_, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance + 20 WHERE id = 1`)
		fmt.Printf("B: попытка %d: %s\n", attempt, code(err))
		if done != nil {
			// B откачен — A получил строку 2 и завершает перевод.
			fmt.Println("A:", code(<-done), "→ COMMIT:", code(a.tx.Commit()))
		}
		return err
	})
	if err != nil {
		return err
	}
	rows, err := db.QueryContext(ctx, `SELECT id, balance FROM accounts ORDER BY id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var id, balance int
		if err := rows.Scan(&id, &balance); err != nil {
			return err
		}
		fmt.Printf("счёт %d: %d\n", id, balance)
	}
	return rows.Err()
}

// Повтор — всей транзакции, а не одного запроса: после
// 1213 InnoDB уже откатил всё, что сделала транзакция.
// 1205 (истёк `innodb_lock_wait_timeout`) откатывает только
// последний запрос, но повторять и её надёжнее целиком.
func retry(ctx context.Context, db *sql.DB, attempts int, fn func(*sql.Tx) error) error {
	var err error
	for range attempts {
		err = inTx(ctx, db, fn)
		var me *mysql.MySQLError
		if !errors.As(err, &me) || (me.Number != 1213 && me.Number != 1205) {
			return err
		}
	}
	return err
}

func inTx(ctx context.Context, db *sql.DB, fn func(*sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

// session — транзакция на своём соединении и номер потока
// этого соединения в `performance_schema`.
type session struct {
	conn   *sql.Conn
	tx     *sql.Tx
	thread int64
}

func begin(ctx context.Context, db *sql.DB, iso sql.IsolationLevel) (*session, error) {
	conn, err := db.Conn(ctx)
	if err != nil {
		return nil, err
	}
	s := &session{conn: conn}
	err = conn.QueryRowContext(ctx, `SELECT PS_CURRENT_THREAD_ID()`).Scan(&s.thread)
	if err == nil {
		s.tx, err = conn.BeginTx(ctx, &sql.TxOptions{Isolation: iso})
	}
	if err != nil {
		conn.Close()
		return nil, err
	}
	return s, nil
}

func (s *session) close() {
	s.tx.Rollback()
	s.conn.Close()
}

// execAsync выполняет запрос в горутине: он может ждать
// блокировку, а главная горутина тем временем идёт дальше.
func (s *session) execAsync(ctx context.Context, q string) <-chan error {
	done := make(chan error, 1)
	go func() {
		_, err := s.tx.ExecContext(ctx, q)
		done <- err
	}()
	return done
}

// locks — построчные блокировки сессии: режим и ключ.
func (s *session) locks(ctx context.Context, db *sql.DB) []string {
	rows, err := db.QueryContext(ctx, `SELECT CONCAT(LOCK_MODE, ' ', LOCK_DATA) FROM performance_schema.data_locks
		WHERE THREAD_ID = ? AND LOCK_TYPE = 'RECORD' ORDER BY LOCK_DATA + 0`, s.thread)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var l string
		if rows.Scan(&l) == nil {
			out = append(out, l)
		}
	}
	return out
}

// waiting ждёт, пока у сессии появится блокировка в
// состоянии WAITING, и возвращает её режим и ключ.
func waiting(ctx context.Context, db *sql.DB, s *session) string {
	for range 500 {
		var l string
		err := db.QueryRowContext(ctx, `SELECT CONCAT(LOCK_MODE, ' ', LOCK_DATA) FROM performance_schema.data_locks
			WHERE THREAD_ID = ? AND LOCK_STATUS = 'WAITING'`, s.thread).Scan(&l)
		if err == nil {
			return l
		}
		time.Sleep(5 * time.Millisecond) // опрос, а не упорядочивание
	}
	return "— не дождались"
}

// code — номер ошибки MySQL вместо текста: так её и
// классифицируют.
func code(err error) string {
	var me *mysql.MySQLError
	switch {
	case err == nil:
		return "ok"
	case errors.As(err, &me):
		return fmt.Sprintf("ошибка %d (%s)", me.Number, me.SQLState)
	default:
		return err.Error()
	}
}
