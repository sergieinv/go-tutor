package task

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
)

// Schema — таблица счетов. Её создают тесты.
const Schema = `CREATE TABLE accounts (
	id      INTEGER PRIMARY KEY,
	owner   TEXT NOT NULL,
	email   TEXT,
	balance INTEGER NOT NULL CHECK (balance >= 0)
)`

// Account — строка таблицы accounts.
type Account struct {
	ID      int64
	Owner   string
	Email   *string // NULL → nil
	Balance int64
}

var (
	ErrNotFound          = errors.New("account not found")
	ErrInsufficientFunds = errors.New("insufficient funds")
	ErrInvalidAmount     = errors.New("invalid amount")
)

// Repo — доступ к счетам поверх пула соединений.
type Repo struct {
	db *sql.DB
}

func NewRepo(db *sql.DB) *Repo {
	return &Repo{db: db}
}

// Create добавляет счёт и возвращает его id.
func (r *Repo) Create(ctx context.Context, owner string, email *string, balance int64) (int64, error) {
	if balance < 0 {
		return 0, ErrInvalidAmount
	}
	// nil-указатель драйвер запишет как NULL.
	res, err := r.db.ExecContext(ctx,
		`INSERT INTO accounts (owner, email, balance) VALUES (?, ?, ?)`, owner, email, balance)
	if err != nil {
		return 0, fmt.Errorf("create account: %w", err)
	}
	return res.LastInsertId()
}

// Get возвращает счёт по id.
func (r *Repo) Get(ctx context.Context, id int64) (Account, error) {
	var a Account
	err := r.db.QueryRowContext(ctx,
		`SELECT id, owner, email, balance FROM accounts WHERE id = ?`, id,
	).Scan(&a.ID, &a.Owner, &a.Email, &a.Balance)
	if errors.Is(err, sql.ErrNoRows) {
		return Account{}, fmt.Errorf("account %d: %w", id, ErrNotFound)
	}
	if err != nil {
		return Account{}, fmt.Errorf("get account %d: %w", id, err)
	}
	return a, nil
}

// List возвращает счета с балансом не меньше minBalance по возрастанию id.
func (r *Repo) List(ctx context.Context, minBalance int64) ([]Account, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT id, owner, email, balance FROM accounts WHERE balance >= ? ORDER BY id`, minBalance)
	if err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	defer rows.Close() // иначе соединение не вернётся в пул
	var out []Account
	for rows.Next() {
		var a Account
		if err := rows.Scan(&a.ID, &a.Owner, &a.Email, &a.Balance); err != nil {
			return nil, fmt.Errorf("list accounts: %w", err)
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list accounts: %w", err)
	}
	return out, nil
}

// Transfer переводит amount со счёта from на счёт to в одной транзакции.
func (r *Repo) Transfer(ctx context.Context, from, to, amount int64) error {
	if amount <= 0 || from == to {
		return ErrInvalidAmount
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// После Commit откат ничего не делает; при раннем выходе — отменяет всё.
	defer tx.Rollback()

	var balance int64
	err = tx.QueryRowContext(ctx, `SELECT balance FROM accounts WHERE id = ?`, from).Scan(&balance)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("account %d: %w", from, ErrNotFound)
	}
	if err != nil {
		return err
	}
	var exists int
	err = tx.QueryRowContext(ctx, `SELECT 1 FROM accounts WHERE id = ?`, to).Scan(&exists)
	if errors.Is(err, sql.ErrNoRows) {
		return fmt.Errorf("account %d: %w", to, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if balance < amount {
		return ErrInsufficientFunds
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance - ? WHERE id = ?`, amount, from); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE accounts SET balance = balance + ? WHERE id = ?`, amount, to); err != nil {
		return err
	}
	return tx.Commit()
}
