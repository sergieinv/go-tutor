// `database/sql` — общий интерфейс к SQL-базам, драйвер
// подключается импортом с `_`. Здесь SQLite на чистом Go
// (`modernc.org/sqlite`, имя драйвера `"sqlite"`).

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite" // регистрирует драйвер "sqlite"
)

func main() {
	ctx := context.Background()

	// `sql.Open` ничего не подключает — только готовит пул.
	// `*sql.DB` создают один раз на программу и делят между
	// горутинами. Проверить связь — `PingContext`.
	dir, _ := os.MkdirTemp("", "shop")
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "shop.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatal(err)
	}

	// `ExecContext` — для запросов без строк результата.
	_, err = db.ExecContext(ctx, `CREATE TABLE accounts (
		id      INTEGER PRIMARY KEY,
		owner   TEXT NOT NULL,
		email   TEXT,
		balance INTEGER NOT NULL)`)
	if err != nil {
		log.Fatal(err)
	}

	// Значения — только через плейсхолдеры `?`: драйвер
	// передаёт их отдельно от текста SQL, инъекция невозможна.
	for _, a := range []struct {
		owner string
		email any // nil станет NULL
		sum   int64
	}{{"Анна", "anna@x.io", 100}, {"Борис", nil, 20}, {"Robert'); DROP TABLE accounts;--", nil, 0}} {
		res, err := db.ExecContext(ctx, `INSERT INTO accounts (owner, email, balance) VALUES (?, ?, ?)`, a.owner, a.email, a.sum)
		if err != nil {
			log.Fatal(err)
		}
		id, _ := res.LastInsertId()
		fmt.Println("вставлен id", id)
	}

	// `QueryRowContext` — ровно одна строка. Ошибку отдаёт
	// `Scan`; нет строки — `sql.ErrNoRows`.
	var owner string
	err = db.QueryRowContext(ctx, `SELECT owner FROM accounts WHERE id = ?`, 42).Scan(&owner)
	fmt.Println("id 42:", errors.Is(err, sql.ErrNoRows))

	// `QueryContext` — много строк. `defer rows.Close()`
	// возвращает соединение в пул, `rows.Err()` — ошибка,
	// прервавшая обход. NULL читаем в `sql.NullString`.
	rows, err := db.QueryContext(ctx, `SELECT id, owner, email, balance FROM accounts ORDER BY id`)
	if err != nil {
		log.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var id, balance int64
		var email sql.NullString
		if err := rows.Scan(&id, &owner, &email, &balance); err != nil {
			log.Fatal(err)
		}
		fmt.Printf("%d %q email=%q (valid=%v) balance=%d\n", id, owner, email.String, email.Valid, balance)
	}
	if err := rows.Err(); err != nil {
		log.Fatal(err)
	}

	// Транзакция: `defer tx.Rollback()` откатит всё при любом
	// раннем выходе, а после `Commit` ничего не сделает.
	fmt.Println("перевод 30:", transfer(ctx, db, 1, 2, 30))
	fmt.Println("перевод 500:", transfer(ctx, db, 1, 2, 500))
	var a, b int64
	db.QueryRowContext(ctx, `SELECT
		(SELECT balance FROM accounts WHERE id = 1),
		(SELECT balance FROM accounts WHERE id = 2)`).Scan(&a, &b)
	fmt.Println("балансы:", a, b)
}

func transfer(ctx context.Context, db *sql.DB, from, to, amount int64) error {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// Списываем, только если хватает денег: проверка и
	// изменение — одним запросом, `RowsAffected` скажет итог.
	res, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance - ? WHERE id = ? AND balance >= ?`, amount, from, amount)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return errors.New("недостаточно средств")
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance + ? WHERE id = ?`, amount, to); err != nil {
		return err
	}
	return tx.Commit()
}
