package task

import (
	"context"
	"database/sql"
	"errors"
)

// Schema — таблицы заказов и outbox; создают тесты.
const Schema = `
CREATE TABLE orders (
	id       INTEGER PRIMARY KEY,
	customer TEXT    NOT NULL,
	total    INTEGER NOT NULL
);
CREATE TABLE outbox (
	id           INTEGER PRIMARY KEY,
	topic        TEXT NOT NULL,
	key          TEXT NOT NULL,
	payload      BLOB NOT NULL,
	published_at TEXT
);`

// TopicOrderCreated — тема события о новом заказе.
const TopicOrderCreated = "order.created"

var ErrInvalidOrder = errors.New("outbox: неверный заказ")

type Event struct {
	ID      int64
	Topic   string
	Key     string
	Payload []byte
}

// OrderCreated — тело события TopicOrderCreated в JSON.
type OrderCreated struct {
	OrderID  int64  `json:"order_id"`
	Customer string `json:"customer"`
	Total    int64  `json:"total"`
}

type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// CreateOrder создаёт заказ и событие о нём в одной транзакции.
func CreateOrder(ctx context.Context, db *sql.DB, customer string, total int64) (int64, error) {
	// TODO
	return 0, errors.New("не реализовано")
}

// Relay публикует события из outbox.
type Relay struct {
	DB        *sql.DB
	Pub       Publisher
	BatchSize int // <= 0 — 100
}

// RunOnce публикует одну пачку неопубликованных событий по порядку
// и возвращает число опубликованных и отмеченных.
func (r *Relay) RunOnce(ctx context.Context) (int, error) {
	// TODO
	return 0, errors.New("не реализовано")
}
