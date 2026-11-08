package task

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
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
	if customer == "" || total <= 0 {
		return 0, ErrInvalidOrder
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, fmt.Errorf("создание заказа: %w", err)
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO orders (customer, total) VALUES (?, ?)`, customer, total)
	if err != nil {
		return 0, fmt.Errorf("создание заказа: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, fmt.Errorf("создание заказа: %w", err)
	}
	payload, err := json.Marshal(OrderCreated{OrderID: id, Customer: customer, Total: total})
	if err != nil {
		return 0, fmt.Errorf("создание заказа: %w", err)
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox (topic, key, payload) VALUES (?, ?, ?)`,
		TopicOrderCreated, strconv.FormatInt(id, 10), payload)
	if err != nil {
		return 0, fmt.Errorf("запись события: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return 0, fmt.Errorf("создание заказа: %w", err)
	}
	return id, nil
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
	batch, err := r.next(ctx)
	if err != nil {
		return 0, err
	}
	for i, e := range batch {
		if err := r.Pub.Publish(ctx, e); err != nil {
			return i, fmt.Errorf("публикация события %d: %w", e.ID, err)
		}
		_, err := r.DB.ExecContext(ctx, `UPDATE outbox SET published_at = CURRENT_TIMESTAMP WHERE id = ?`, e.ID)
		if err != nil {
			return i, fmt.Errorf("отметка события %d: %w", e.ID, err)
		}
	}
	return len(batch), nil
}

// next читает пачку целиком и закрывает Rows до публикации:
// соединение не занято, пока ждём брокер.
func (r *Relay) next(ctx context.Context) ([]Event, error) {
	limit := r.BatchSize
	if limit <= 0 {
		limit = 100
	}
	rows, err := r.DB.QueryContext(ctx, `SELECT id, topic, key, payload FROM outbox
		WHERE published_at IS NULL ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return nil, fmt.Errorf("чтение outbox: %w", err)
	}
	defer rows.Close()
	var batch []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Topic, &e.Key, &e.Payload); err != nil {
			return nil, fmt.Errorf("чтение outbox: %w", err)
		}
		batch = append(batch, e)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("чтение outbox: %w", err)
	}
	return batch, nil
}
