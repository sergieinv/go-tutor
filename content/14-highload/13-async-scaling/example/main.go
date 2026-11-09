// Асинхронная обработка с доставкой «хотя бы один раз»: три
// экземпляра потребителя разбирают партиции очереди и строят модель
// чтения «сумма заказов покупателя». Часть событий приходит повторно
// и к другому экземпляру — как после падения и перебалансировки
// группы потребителей Kafka. Сравниваем три потребителя.

package main

import (
	"context"
	"database/sql"
	"fmt"
	"hash/fnv"
	"log"
	"sync"
	"sync/atomic"

	_ "modernc.org/sqlite"
)

// Event — «заказ создан». ID — ключ идемпотентности: его выдаёт
// производитель, и повтор доставки несёт тот же ID.
type Event struct {
	ID       string
	Customer string
	Amount   int64
}

const instances = 3

func main() {
	ctx := context.Background()
	db, err := sql.Open("sqlite", ":memory:")
	check(err)
	defer db.Close()
	// `:memory:` — своя база на каждое соединение, поэтому одно.
	db.SetMaxOpenConns(1)

	events := []Event{
		{"e1", "anna", 100}, {"e2", "boris", 250}, {"e3", "vera", 300},
		{"e4", "anna", 40}, {"e5", "boris", 10}, {"e6", "anna", 60},
		{"e7", "vera", 5}, {"e8", "boris", 90}, {"e9", "anna", 1},
	}
	work := deliveries(events)

	// Наивный потребитель: просто прибавляет сумму.
	naive := run(ctx, db, work, func(ctx context.Context, db *sql.DB, _ int, e Event) bool {
		check(addToBalance(ctx, db, e))
		return true
	})

	// Дедупликация в памяти экземпляра: map видимых ID у каждого
	// своя. Повтор, пришедший к другому экземпляру, она не видит.
	seen := make([]map[string]bool, instances)
	for i := range seen {
		seen[i] = map[string]bool{}
	}
	local := run(ctx, db, work, func(ctx context.Context, db *sql.DB, inst int, e Event) bool {
		if seen[inst][e.ID] {
			return false
		}
		seen[inst][e.ID] = true
		check(addToBalance(ctx, db, e))
		return true
	})

	// Идемпотентный потребитель: отметка «обработано» и само
	// изменение — в одной транзакции. Повтор упрётся в первичный
	// ключ `processed`, и изменение не применится. Состояние — в
	// базе, поэтому неважно, какой экземпляр получил событие.
	durable := run(ctx, db, work, func(ctx context.Context, db *sql.DB, _ int, e Event) bool {
		tx, err := db.BeginTx(ctx, nil)
		check(err)
		defer tx.Rollback()
		res, err := tx.ExecContext(ctx,
			`INSERT INTO processed (event_id) VALUES (?) ON CONFLICT DO NOTHING`, e.ID)
		check(err)
		if n, _ := res.RowsAffected(); n == 0 {
			return false // уже обработано — подтверждаем и идём дальше
		}
		check(addToBalance(ctx, tx, e))
		check(tx.Commit())
		return true
	})

	fmt.Println("событий:", len(events), "| доставок:", total(work))
	fmt.Printf("%-18s%s\n", "ожидалось:", expected(events))
	fmt.Printf("%-18s%s\n", "наивный:", naive)
	fmt.Printf("%-18s%s\n", "map в экземпляре:", local)
	fmt.Printf("%-18s%s\n", "в базе:", durable)
}

// deliveries раскладывает события по партициям (по покупателю —
// порядок событий одного покупателя сохраняется) и дописывает
// повторы: каждое третье событие доставлено ещё раз, причём
// следующему экземпляру — его партиция переехала.
func deliveries(events []Event) [][]Event {
	work := make([][]Event, instances)
	for _, e := range events {
		p := partition(e.Customer)
		work[p] = append(work[p], e)
	}
	for i, e := range events {
		if i%3 == 2 {
			next := (partition(e.Customer) + 1) % instances
			work[next] = append(work[next], e)
		}
	}
	return work
}

func partition(key string) int {
	h := fnv.New32a()
	h.Write([]byte(key))
	return int(h.Sum32() % instances)
}

type handler func(ctx context.Context, db *sql.DB, instance int, e Event) (applied bool)

// run пересоздаёт таблицы, запускает по горутине на экземпляр и
// возвращает модель чтения — «сумма/заказов» по покупателям — и
// сколько доставок отброшено как повтор.
func run(ctx context.Context, db *sql.DB, work [][]Event, handle handler) string {
	_, err := db.ExecContext(ctx, `DROP TABLE IF EXISTS processed; DROP TABLE IF EXISTS balances;
		CREATE TABLE processed (event_id TEXT PRIMARY KEY);
		CREATE TABLE balances (customer TEXT PRIMARY KEY, total INTEGER NOT NULL, orders INTEGER NOT NULL)`)
	check(err)
	var wg sync.WaitGroup
	var skipped atomic.Int32
	for inst, part := range work {
		wg.Go(func() {
			for _, e := range part {
				if !handle(ctx, db, inst, e) {
					skipped.Add(1)
				}
			}
		})
	}
	wg.Wait()
	rows, err := db.QueryContext(ctx, `SELECT customer, total, orders FROM balances ORDER BY customer`)
	check(err)
	defer rows.Close()
	out := ""
	for rows.Next() {
		var c string
		var t, n int64
		check(rows.Scan(&c, &t, &n))
		out += fmt.Sprintf(" %s=%d/%d", c, t, n)
	}
	check(rows.Err())
	return fmt.Sprintf("%s | повторов отброшено: %d", out, skipped.Load())
}

type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// addToBalance — проекция события в модель чтения (CQRS): сумма и
// число заказов покупателя, готовые для экрана без JOIN и GROUP BY.
func addToBalance(ctx context.Context, db execer, e Event) error {
	_, err := db.ExecContext(ctx, `INSERT INTO balances (customer, total, orders) VALUES (?, ?, 1)
		ON CONFLICT (customer) DO UPDATE SET total = total + excluded.total, orders = orders + 1`,
		e.Customer, e.Amount)
	return err
}

func expected(events []Event) string {
	sum := map[string][2]int64{}
	for _, e := range events {
		s := sum[e.Customer]
		sum[e.Customer] = [2]int64{s[0] + e.Amount, s[1] + 1}
	}
	out := ""
	for _, c := range []string{"anna", "boris", "vera"} {
		out += fmt.Sprintf(" %s=%d/%d", c, sum[c][0], sum[c][1])
	}
	return out
}

func total(work [][]Event) (n int) {
	for _, p := range work {
		n += len(p)
	}
	return n
}

func check(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
