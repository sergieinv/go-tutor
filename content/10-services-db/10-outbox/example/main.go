// Transactional outbox: событие пишется в таблицу `outbox`
// той же транзакцией, что и заказ, а отдельный relay потом
// публикует его в брокер. Брокер здесь — фейк (Kafka — в
// модуле 12).

package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"

	_ "modernc.org/sqlite"
)

const schema = `
CREATE TABLE orders (id INTEGER PRIMARY KEY, customer TEXT NOT NULL, total INTEGER NOT NULL);
CREATE TABLE outbox (
	id           INTEGER PRIMARY KEY, -- порядок событий
	topic        TEXT NOT NULL,
	key          TEXT NOT NULL,       -- ключ партиционирования: id заказа
	payload      BLOB NOT NULL,
	published_at TEXT                 -- NULL — ещё не опубликовано
);
CREATE INDEX outbox_unpublished_idx ON outbox (id) WHERE published_at IS NULL;
-- сторона потребителя: какие события уже обработаны
CREATE TABLE processed (event_id INTEGER PRIMARY KEY);
CREATE TABLE points (customer TEXT PRIMARY KEY, total INTEGER NOT NULL);`

type Event struct {
	ID      int64
	Topic   string
	Key     string
	Payload []byte
}

// Publisher — всё, что relay знает о брокере.
type Publisher interface {
	Publish(ctx context.Context, e Event) error
}

// broker — фейк: копит доставленные события, `down` —
// сколько следующих публикаций упадёт.
type broker struct {
	down      int
	delivered []Event
}

func (b *broker) Publish(ctx context.Context, e Event) error {
	if b.down > 0 {
		b.down--
		return errors.New("брокер недоступен")
	}
	b.delivered = append(b.delivered, e)
	return nil
}

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "outbox")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(dir)
	db, err := sql.Open("sqlite", filepath.Join(dir, "shop.db"))
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, schema); err != nil {
		log.Fatal(err)
	}
	b := &broker{}

	// Двойная запись: заказ закоммичен, публикация упала —
	// событие потеряно навсегда. Публикация до коммита не
	// лучше: тогда при откате уйдёт событие о несуществующем
	// заказе. Общей транзакции у базы и брокера нет.
	b.down = 1
	err = createOrderNaive(ctx, db, b, "Анна", 300)
	fmt.Println("двойная запись:", err, "| заказов:", countOrders(db), "| у брокера:", len(b.delivered))

	// Outbox: заказ и событие — одна транзакция. Либо оба,
	// либо ничего; брокер в ней не участвует.
	for _, o := range []struct {
		customer string
		total    int64
	}{{"Борис", 450}, {"Вера", 120}} {
		if _, err := createOrder(ctx, db, o.customer, o.total); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Println("в outbox ждут:", unpublished(db))

	// Relay упал на публикации: событие не помечено и уйдёт
	// в следующий раз. Следующие события он не трогает —
	// иначе нарушится порядок.
	b.down = 1
	n, err := relayOnce(ctx, db, b, 100)
	fmt.Println("relay:", n, err, "| ждут:", unpublished(db))
	n, err = relayOnce(ctx, db, b, 100)
	fmt.Println("relay:", n, err, "| ждут:", unpublished(db))

	// Relay упал между `Publish` и `UPDATE` (имитируем,
	// сняв отметку): событие уйдёт повторно. Гарантия —
	// at-least-once, а не exactly-once.
	db.ExecContext(ctx, `UPDATE outbox SET published_at = NULL WHERE id = 2`)
	n, err = relayOnce(ctx, db, b, 100)
	fmt.Println("relay после сбоя:", n, err)

	// Поэтому потребитель идемпотентен: id события и эффект
	// записываются в одной транзакции, дубль пропускается.
	for _, e := range b.delivered {
		applied, err := consume(ctx, db, e)
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("  событие %d (%s, key=%s): применено=%v\n", e.ID, e.Topic, e.Key, applied)
	}
	var points int64
	db.QueryRowContext(ctx, `SELECT total FROM points WHERE customer = 'Вера'`).Scan(&points)
	fmt.Println("баллы Веры:", points)

	// Очистка: опубликованное удаляют (в продакшне — старше
	// срока хранения), иначе таблица растёт бесконечно.
	res, _ := db.ExecContext(ctx, `DELETE FROM outbox WHERE published_at IS NOT NULL`)
	deleted, _ := res.RowsAffected()
	fmt.Println("очищено:", deleted)
}

// createOrderNaive — так делать не надо: две независимые записи.
func createOrderNaive(ctx context.Context, db *sql.DB, pub Publisher, customer string, total int64) error {
	res, err := db.ExecContext(ctx, `INSERT INTO orders (customer, total) VALUES (?, ?)`, customer, total)
	if err != nil {
		return err
	}
	id, _ := res.LastInsertId()
	return pub.Publish(ctx, Event{Topic: "order.created", Key: strconv.FormatInt(id, 10)})
}

func createOrder(ctx context.Context, db *sql.DB, customer string, total int64) (int64, error) {
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO orders (customer, total) VALUES (?, ?)`, customer, total)
	if err != nil {
		return 0, fmt.Errorf("заказ: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	payload, err := json.Marshal(struct {
		OrderID  int64  `json:"order_id"`
		Customer string `json:"customer"`
		Total    int64  `json:"total"`
	}{id, customer, total})
	if err != nil {
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO outbox (topic, key, payload) VALUES ('order.created', ?, ?)`,
		strconv.FormatInt(id, 10), payload)
	if err != nil {
		return 0, fmt.Errorf("outbox: %w", err)
	}
	return id, tx.Commit()
}

// relayOnce публикует пачку неотмеченных событий по порядку
// и останавливается на первой ошибке. Выборка дочитывается
// и закрывается до публикации: соединение не держим, пока
// ждём брокер.
func relayOnce(ctx context.Context, db *sql.DB, pub Publisher, limit int) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, topic, key, payload FROM outbox
		WHERE published_at IS NULL ORDER BY id LIMIT ?`, limit)
	if err != nil {
		return 0, err
	}
	var batch []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.Topic, &e.Key, &e.Payload); err != nil {
			rows.Close()
			return 0, err
		}
		batch = append(batch, e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	for i, e := range batch {
		if err := pub.Publish(ctx, e); err != nil {
			return i, fmt.Errorf("публикация события %d: %w", e.ID, err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE outbox SET published_at = CURRENT_TIMESTAMP WHERE id = ?`, e.ID); err != nil {
			return i, fmt.Errorf("отметка события %d: %w", e.ID, err)
		}
	}
	return len(batch), nil
}

// consume — идемпотентный обработчик: начисляет баллы за
// заказ ровно один раз на событие.
func consume(ctx context.Context, db *sql.DB, e Event) (bool, error) {
	var order struct {
		Customer string `json:"customer"`
		Total    int64  `json:"total"`
	}
	if err := json.Unmarshal(e.Payload, &order); err != nil {
		return false, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT INTO processed (event_id) VALUES (?) ON CONFLICT DO NOTHING`, e.ID)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil // дубль: эффект уже применён
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO points (customer, total) VALUES (?, ?)
		ON CONFLICT (customer) DO UPDATE SET total = total + excluded.total`, order.Customer, order.Total/10)
	if err != nil {
		return false, err
	}
	return true, tx.Commit()
}

func countOrders(db *sql.DB) (n int) {
	db.QueryRow(`SELECT count(*) FROM orders`).Scan(&n)
	return n
}

func unpublished(db *sql.DB) (n int) {
	db.QueryRow(`SELECT count(*) FROM outbox WHERE published_at IS NULL`).Scan(&n)
	return n
}
