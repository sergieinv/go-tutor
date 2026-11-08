// `*sql.DB` — не соединение, а пул соединений: он сам
// открывает их по требованию, выдаёт на время запроса и
// принимает обратно. Пример настраивает пул, исчерпывает его,
// показывает очередь ожидания и три типичные утечки.
// SQLite лежит в файле: так у пула может быть несколько
// соединений к одной базе.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "pool")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "shop.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Настройки пула — сразу после `Open`. По умолчанию
	// открытых соединений сколько угодно, а простаивающих
	// хранится два. Время жизни и простоя ограничивают,
	// чтобы соединения обновлялись (балансировщик, failover).
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(3)
	db.SetConnMaxLifetime(30 * time.Minute)
	db.SetConnMaxIdleTime(5 * time.Minute)
	stats("после Open", db)

	// Соединения открываются лениво — первым запросом.
	// После запроса соединение не закрывается, а ждёт
	// следующего в простаивающих (`Idle`).
	mustExec(ctx, db, `CREATE TABLE items (id INTEGER PRIMARY KEY, name TEXT)`)
	mustExec(ctx, db, `INSERT INTO items (name) VALUES ('чай'), ('кофе'), ('какао')`)
	stats("после запросов", db)

	// `db.Conn` берёт соединение «в аренду» до `Close`.
	// Займём все три — пул исчерпан.
	var held []*sql.Conn
	for range 3 {
		c, err := db.Conn(ctx)
		if err != nil {
			log.Fatal(err)
		}
		held = append(held, c)
	}
	stats("заняты все", db)

	// Четвёртый запрос встаёт в очередь. Ждать он будет,
	// пока не освободится соединение или не кончится
	// контекст, — без дедлайна ждал бы вечно.
	tctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
	_, err = db.ExecContext(tctx, `SELECT 1`)
	cancel()
	fmt.Println("четвёртый запрос:", errors.Is(err, context.DeadlineExceeded))

	// Вернули соединение — его сразу получает ждущий.
	// `WaitCount` и `WaitDuration` растут: это главный
	// признак того, что пулу не хватает соединений.
	done := make(chan error)
	go func() {
		var n int
		done <- db.QueryRowContext(ctx, `SELECT count(*) FROM items`).Scan(&n)
	}()
	waitForWaiters(db, 2)
	held[0].Close()
	fmt.Println("ждущий дождался:", <-done)
	held[1].Close()
	held[2].Close()
	s := db.Stats()
	stats("всё вернули", db)
	fmt.Println("WaitDuration > 0:", s.WaitDuration > 0)

	// Утечки: каждая функция ниже работает, но оставляет
	// соединение занятым навсегда. Три таких вызова — и
	// пул из трёх соединений исчерпан.
	db.QueryRowContext(ctx, `DELETE FROM items WHERE id = ?`, 3)
	fmt.Println("QueryRow без Scan: InUse =", db.Stats().InUse)
	firstLeaky(ctx, db)
	fmt.Println("Rows без Close: InUse =", db.Stats().InUse)
	renameLeaky(ctx, db, 1, "")
	fmt.Println("Tx без Rollback: InUse =", db.Stats().InUse)
	tctx, cancel = context.WithTimeout(ctx, 50*time.Millisecond)
	defer cancel()
	_, err = db.ExecContext(tctx, `SELECT 1`)
	fmt.Println("следующий запрос:", err)
}

// firstLeaky возвращает первое имя, но на раннем `return`
// не закрывает `rows`: `Next` закрыл бы их сам, только дойдя
// до конца выборки. Лечится `defer rows.Close()` сразу
// после проверки ошибки.
func firstLeaky(ctx context.Context, db *sql.DB) (string, error) {
	rows, err := db.QueryContext(ctx, `SELECT name FROM items ORDER BY id`)
	if err != nil {
		return "", err
	}
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return "", err // и здесь rows не закрыт
		}
		if name != "" {
			return name, nil // rows не закрыт — соединение занято
		}
	}
	rows.Close()
	return "", rows.Err()
}

// renameLeaky на ошибке выходит, не завершив транзакцию:
// она держит своё соединение, пока жив контекст. Лечится
// `defer tx.Rollback()` сразу после `BeginTx`.
func renameLeaky(ctx context.Context, db *sql.DB, id int64, name string) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if name == "" {
		return errors.New("пустое имя") // транзакция брошена
	}
	if _, err := tx.ExecContext(ctx, `UPDATE items SET name = ? WHERE id = ?`, name, id); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// waitForWaiters ждёт, пока в очереди пула окажется n-й
// ждущий за всё время: так порядок шагов не зависит от
// планировщика.
func waitForWaiters(db *sql.DB, n int64) {
	for db.Stats().WaitCount < n {
		time.Sleep(time.Millisecond)
	}
}

func stats(when string, db *sql.DB) {
	s := db.Stats()
	fmt.Printf("%-15s open=%d inUse=%d idle=%d waitCount=%d\n", when+":", s.OpenConnections, s.InUse, s.Idle, s.WaitCount)
}

func mustExec(ctx context.Context, db *sql.DB, q string) {
	if _, err := db.ExecContext(ctx, q); err != nil {
		log.Fatal(err)
	}
}
