package task

import (
	"context"
	"database/sql"
	"errors"
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
	// TODO: реализуйте метод
	return 0, nil
}

// Get возвращает счёт по id.
func (r *Repo) Get(ctx context.Context, id int64) (Account, error) {
	// TODO: реализуйте метод
	return Account{}, nil
}

// List возвращает счета с балансом не меньше minBalance по возрастанию id.
func (r *Repo) List(ctx context.Context, minBalance int64) ([]Account, error) {
	// TODO: реализуйте метод
	return nil, nil
}

// Transfer переводит amount со счёта from на счёт to в одной транзакции.
func (r *Repo) Transfer(ctx context.Context, from, to, amount int64) error {
	// TODO: реализуйте метод
	return nil
}
