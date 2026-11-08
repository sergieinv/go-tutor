// В Go нет `TransactionScope` и «текущей транзакции» в
// потоке: транзакция — значение `*sql.Tx`, и каждый запрос
// явно идёт либо через пул `*sql.DB`, либо через `tx`.
// Пример: общий интерфейс для обоих, хелпер `withTx`,
// `NULL` при записи и чтении и дедлок от `db` внутри `tx`.

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

// `DBTX` — то общее, что есть у `*sql.DB` и `*sql.Tx`.
// Функции хранилища принимают его и не знают, вызваны
// они в транзакции или нет: решает вызывающий.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

var ErrInsufficient = errors.New("недостаточно средств")

// `sql.Null[string]` без `Valid` пишется как `NULL`.
func addEntry(ctx context.Context, q DBTX, account, amount int64, memo sql.Null[string]) error {
	_, err := q.ExecContext(ctx, `INSERT INTO ledger (account_id, amount, memo) VALUES (?, ?, ?)`, account, amount, memo)
	return err
}

func withdraw(ctx context.Context, q DBTX, account, amount int64) error {
	res, err := q.ExecContext(ctx, `UPDATE accounts SET balance = balance - ? WHERE id = ? AND balance >= ?`, amount, account, amount)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return ErrInsufficient
	}
	return addEntry(ctx, q, account, -amount, sql.Null[string]{})
}

// `withTx` — единственное место, где начинают и завершают
// транзакции. `defer tx.Rollback()` откатывает при ошибке
// `fn` и при панике (`defer` выполняется и тогда, а паника
// летит дальше), а после `Commit` ничего не делает.
func withTx(ctx context.Context, db *sql.DB, opts *sql.TxOptions, fn func(tx *sql.Tx) error) error {
	tx, err := db.BeginTx(ctx, opts)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "tx")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "bank.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	_, err = db.ExecContext(ctx, `
		CREATE TABLE accounts (id INTEGER PRIMARY KEY, balance INTEGER NOT NULL);
		CREATE TABLE ledger (id INTEGER PRIMARY KEY, account_id INTEGER NOT NULL,
			amount INTEGER NOT NULL, memo TEXT);
		INSERT INTO accounts VALUES (1, 100), (2, 0);`)
	if err != nil {
		log.Fatal(err)
	}

	// Перевод — два списания/зачисления и две записи журнала
	// в одной транзакции: все функции получают `tx`.
	transfer := func(from, to, amount int64) error {
		return withTx(ctx, db, nil, func(tx *sql.Tx) error {
			if err := withdraw(ctx, tx, from, amount); err != nil {
				return err
			}
			if _, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance + ? WHERE id = ?`, amount, to); err != nil {
				return err
			}
			return addEntry(ctx, tx, to, amount, sql.Null[string]{V: "перевод", Valid: true})
		})
	}
	fmt.Println("перевод 30:", transfer(1, 2, 30))
	fmt.Println("перевод 500:", transfer(1, 2, 500))

	// Паника внутри — транзакция откатилась, паника дошла
	// до вызывающего.
	func() {
		defer func() { fmt.Println("паника:", recover()) }()
		withTx(ctx, db, nil, func(tx *sql.Tx) error {
			withdraw(ctx, tx, 1, 10)
			panic("сбой посреди перевода")
		})
	}()

	// Без транзакции те же функции работают через пул.
	if err := addEntry(ctx, db, 2, 0, sql.Null[string]{}); err != nil {
		log.Fatal(err)
	}

	// `NULL` читаем в `sql.Null[string]` или заменяем
	// в самом запросе через `COALESCE`.
	rows, err := db.QueryContext(ctx, `SELECT account_id, amount, memo, COALESCE(memo, '—') FROM ledger ORDER BY id`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var account, amount int64
		var memo sql.Null[string]
		var shown string
		if err := rows.Scan(&account, &amount, &memo, &shown); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("счёт %d %+4d memo.Valid=%-5v %s\n", account, amount, memo.Valid, shown)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}
	var b1, b2 int64
	db.QueryRowContext(ctx, `SELECT (SELECT balance FROM accounts WHERE id = 1), (SELECT balance FROM accounts WHERE id = 2)`).Scan(&b1, &b2)
	fmt.Println("балансы:", b1, b2)

	// Ошибка: запрос через `db` внутри транзакции. Он идёт
	// в другое соединение — мимо транзакции, а при пуле из
	// одного соединения ждёт его вечно. Здесь ожидание
	// ограничено таймаутом.
	db.SetMaxOpenConns(1)
	err = withTx(ctx, db, nil, func(tx *sql.Tx) error {
		tctx, cancel := context.WithTimeout(ctx, 50*time.Millisecond)
		defer cancel()
		return withdraw(tctx, db, 1, 10) // надо: tx
	})
	fmt.Println("db внутри tx:", err)
}
