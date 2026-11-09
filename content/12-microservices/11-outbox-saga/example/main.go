// Outbox из модуля 10 теперь публикует в Kafka, а заказ,
// которому нужны склад и оплата, оформляется сагой:
// шаги в чужих сервисах и компенсации в обратном порядке.

package main

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"slices"
	"strconv"

	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
	_ "modernc.org/sqlite"
)

const topic = "orders.events"

const schema = `CREATE TABLE outbox (
	id           INTEGER PRIMARY KEY,
	key          TEXT NOT NULL,
	type         TEXT NOT NULL,
	published_at TEXT
);`

func main() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // ":memory:" — своя база на каждое соединение
	if _, err := db.ExecContext(ctx, schema); err != nil {
		log.Fatal(err)
	}
	// События пишет сервис заказов в своих транзакциях
	// (урок 10 модуля 10); здесь — сразу готовые строки.
	for _, e := range [][2]string{{"A-1", "created"}, {"B-2", "created"}, {"A-1", "paid"}} {
		if _, err := db.ExecContext(ctx, `INSERT INTO outbox (key, type) VALUES (?, ?)`, e[0], e[1]); err != nil {
			log.Fatal(err)
		}
	}

	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(3, topic))
	if err != nil {
		log.Fatal(err)
	}
	defer cluster.Close()
	cl, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()

	// Брокер отказывает партиции, куда попадает B-2 (p1):
	// A-1 created и A-1 paid записаны, B-2 — нет.
	cluster.Fault(kfake.Fault{Keys: []kmsg.Key{kmsg.Produce}, Topic: topic, Partitions: []int32{1}, Err: kerr.InvalidRecord})
	n, err := relayOnce(ctx, db, cl)
	fmt.Printf("relay: отмечено %d, ошибка: %v\n", n, err != nil)
	n, err = relayOnce(ctx, db, cl)
	fmt.Printf("relay: отмечено %d, ошибка: %v\n", n, err)
	printTopic(ctx, cluster)

	sagaDemo()
}

// relayOnce публикует пачку одним ProduceSync и отмечает
// события по порядку id до первой неудачной записи.
// Удачные записи после неё уйдут повторно — at-least-once.
func relayOnce(ctx context.Context, db *sql.DB, cl *kgo.Client) (int, error) {
	rows, err := db.QueryContext(ctx, `SELECT id, key, type FROM outbox
		WHERE published_at IS NULL ORDER BY id LIMIT 100`)
	if err != nil {
		return 0, err
	}
	var ids []int64
	var recs []*kgo.Record
	for rows.Next() {
		var id int64
		var key, typ string
		if err := rows.Scan(&id, &key, &typ); err != nil {
			rows.Close()
			return 0, err
		}
		ids = append(ids, id)
		// Ключ — id заказа: события заказа в одной партиции,
		// порядок по ключу сохраняется. id строки — event-id
		// для дедупликации у потребителя.
		recs = append(recs, &kgo.Record{Topic: topic, Key: []byte(key), Value: []byte(typ),
			Headers: []kgo.RecordHeader{{Key: "event-id", Value: []byte(strconv.FormatInt(id, 10))}}})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// Ошибку каждой записи ищем по указателю: результаты
	// ProduceSync идут в порядке подтверждений.
	failed := map[*kgo.Record]error{}
	for _, res := range cl.ProduceSync(ctx, recs...) {
		if res.Err != nil {
			failed[res.Record] = res.Err
		}
	}
	for i, r := range recs {
		if err := failed[r]; err != nil {
			return i, fmt.Errorf("событие %d: %w", ids[i], err)
		}
		if _, err := db.ExecContext(ctx, `UPDATE outbox SET published_at = CURRENT_TIMESTAMP WHERE id = ?`, ids[i]); err != nil {
			return i, err
		}
	}
	return len(recs), nil
}

func printTopic(ctx context.Context, cluster *kfake.Cluster) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...), kgo.ConsumeTopics(topic))
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	var lines []string
	for len(lines) < 4 {
		fetches := cl.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			log.Fatal(err)
		}
		fetches.EachRecord(func(r *kgo.Record) {
			lines = append(lines, fmt.Sprintf("  p%d@%d %s %s event-id=%s", r.Partition, r.Offset, r.Key, r.Value, r.Headers[0].Value))
		})
	}
	slices.Sort(lines)
	fmt.Println("в топике (A-1 paid — дважды, потребитель отсечёт по event-id):")
	for _, l := range lines {
		fmt.Println(l)
	}
}

// step — шаг саги и его компенсация.
type step struct {
	name       string
	do         func() error
	compensate func() error
}

// runSaga — оркестратор: шаги по порядку, при сбое —
// компенсации выполненных шагов в обратном порядке.
// Настоящий оркестратор сохраняет состояние после каждого
// шага (см. задание), чтобы продолжить после перезапуска.
func runSaga(steps []step) error {
	var done []step
	for _, s := range steps {
		if err := s.do(); err != nil {
			fmt.Printf("  %s: ОШИБКА %v — компенсируем\n", s.name, err)
			for _, d := range slices.Backward(done) {
				if cerr := d.compensate(); cerr != nil {
					// Полууспех: дальше без человека не обойтись.
					return fmt.Errorf("компенсация %s: %w (после: %w)", d.name, cerr, err)
				}
				fmt.Printf("  %s: компенсировано\n", d.name)
			}
			return fmt.Errorf("%s: %w", s.name, err)
		}
		fmt.Printf("  %s: ok\n", s.name)
		done = append(done, s)
	}
	return nil
}

func sagaDemo() {
	ok := func() error { return nil }
	declined := func() error { return errors.New("карта отклонена") }
	steps := func(pay, cancelOrder func() error) []step {
		return []step{
			{"склад: резерв", ok, ok},            // синхронно, gRPC (модуль 11)
			{"заказы: создать", ok, cancelOrder}, // компенсация — отмена, а не DELETE
			{"оплата: списать", pay, ok},
		}
	}
	fmt.Println("сага 1:")
	fmt.Println("итог:", runSaga(steps(ok, ok)))
	fmt.Println("сага 2:")
	fmt.Println("итог:", runSaga(steps(declined, ok)))
	fmt.Println("сага 3:")
	fmt.Println("итог:", runSaga(steps(declined, func() error { return errors.New("заказы недоступны") })))
}
