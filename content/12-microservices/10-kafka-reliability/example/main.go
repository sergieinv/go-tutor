// Надёжность с двух сторон: идемпотентный продюсер не
// пишет дубли при повторах, а потребитель переживает
// ядовитые сообщения (DLQ) и повторную доставку (таблица
// обработанных ключей).

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
	"github.com/twmb/franz-go/pkg/kmsg"
)

const (
	topic = "orders.events"
	dlq   = "orders.events.dlq"
)

func main() {
	ctx := context.Background()

	// Брокер записал пачку, но ответ потерялся (таймаут):
	// клиент повторяет запрос. Fault в kfake имитирует
	// ровно это — REQUEST_TIMED_OUT после записи.
	for _, idempotent := range []bool{true, false} {
		fmt.Printf("идемпотентный=%v: записей в партиции %d\n", idempotent, lostAck(ctx, idempotent))
	}

	// Идемпотентность требует acks=all: подтверждение только
	// после записи на все реплики из ISR.
	_, err := kgo.NewClient(kgo.SeedBrokers("127.0.0.1:1"), kgo.RequiredAcks(kgo.LeaderAck()))
	fmt.Println("acks=1:", err)

	consumerSide(ctx)
}

// lostAck отправляет одну запись при потерянном ответе и
// возвращает, сколько записей оказалось в партиции.
func lostAck(ctx context.Context, idempotent bool) int64 {
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, topic))
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	opts := []kgo.Opt{kgo.SeedBrokers(c.ListenAddrs()...)}
	if !idempotent {
		opts = append(opts, kgo.DisableIdempotentWrite())
	}
	cl, err := kgo.NewClient(opts...)
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	c.Fault(kfake.Fault{Keys: []kmsg.Key{kmsg.Produce}, Err: kerr.RequestTimedOut})
	if err := cl.ProduceSync(ctx, &kgo.Record{Topic: topic, Value: []byte("paid")}).FirstErr(); err != nil {
		log.Fatal(err)
	}
	// Идемпотентный продюсер шлёт id продюсера и номер
	// последовательности — брокер узнаёт повтор и не пишет
	// его второй раз. Без этого повтор — второе сообщение.
	ends, err := kadm.NewClient(cl).ListEndOffsets(ctx, topic)
	if err != nil {
		log.Fatal(err)
	}
	end, _ := ends.Lookup(topic, 0)
	return end.Offset
}

type OrderEvent struct {
	OrderID string `json:"order_id"`
	Type    string `json:"type"`
}

func event(id, value string) *kgo.Record {
	return &kgo.Record{Topic: topic, Key: []byte("A-1"), Value: []byte(value),
		// Ключ идемпотентности — id события, а не ключ
		// записи: у заказа A-1 много разных событий.
		Headers: []kgo.RecordHeader{{Key: "event-id", Value: []byte(id)}}}
}

func header(r *kgo.Record, key string) string {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

func consumerSide(ctx context.Context) {
	c, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(1, topic, dlq))
	if err != nil {
		log.Fatal(err)
	}
	defer c.Close()
	cl, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...))
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()

	// Поток: обычное событие, «ядовитое» (не разбирается —
	// повторы не помогут), событие, на котором сосед
	// временно недоступен, и повторная доставка первого.
	stream := []*kgo.Record{
		event("e1", `{"order_id":"A-1","type":"created"}`),
		event("e2", `{"order_id":"A-1",`),
		event("e3", `{"order_id":"A-1","type":"paid"}`),
		event("e1", `{"order_id":"A-1","type":"created"}`),
	}
	for i, r := range stream {
		r.Offset = int64(i) // офсеты, как при чтении из топика
	}

	processed := map[string]bool{} // в проде — таблица в БД, в одной транзакции с эффектом
	flaky := 2                     // столько раз упадёт обработка e3
	handle := func(r *kgo.Record) error {
		var e OrderEvent
		if err := json.Unmarshal(r.Value, &e); err != nil {
			return permanent{err} // ошибка данных — не повторять
		}
		if e.Type == "paid" && flaky > 0 {
			flaky--
			return errors.New("склад недоступен")
		}
		return nil
	}

	const maxAttempts = 3
	for _, r := range stream {
		id := header(r, "event-id")
		if processed[id] {
			fmt.Printf("%s@%d: дубль, пропущен\n", id, r.Offset)
			continue
		}
		// Ограниченные повторы: временная ошибка проходит
		// со второй-третьей попытки, ядовитое сообщение —
		// сразу в DLQ, чтобы не блокировать партицию.
		var err error
		attempt := 0
		for attempt < maxAttempts {
			attempt++
			if err = handle(r); err == nil || errors.As(err, new(permanent)) {
				break
			}
			// здесь — пауза с backoff (урок 5)
		}
		if err != nil {
			toDLQ(ctx, cl, r, err, attempt)
			fmt.Printf("%s@%d: в DLQ (попыток: %d): %v\n", id, r.Offset, attempt, err)
		} else {
			fmt.Printf("%s@%d: обработано с попытки %d\n", id, r.Offset, attempt)
		}
		processed[id] = true // и после DLQ: повторная доставка не должна слать его туда снова
	}

	// В DLQ — исходные ключ и тело, а причина и координаты
	// оригинала — в заголовках: по ним разбирают и
	// переотправляют.
	reader, err := kgo.NewClient(kgo.SeedBrokers(c.ListenAddrs()...), kgo.ConsumeTopics(dlq))
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()
	for _, r := range reader.PollFetches(ctx).Records() {
		fmt.Printf("DLQ: key=%s value=%s\n", r.Key, r.Value)
		for _, h := range r.Headers {
			fmt.Printf("  %s: %s\n", h.Key, h.Value)
		}
	}
}

// permanent помечает ошибку, которую повторять бесполезно.
type permanent struct{ error }

func (p permanent) Unwrap() error { return p.error }

func toDLQ(ctx context.Context, cl *kgo.Client, r *kgo.Record, cause error, attempts int) {
	d := &kgo.Record{
		Topic: dlq,
		Key:   r.Key,
		Value: r.Value,
		Headers: append(r.Headers[:len(r.Headers):len(r.Headers)],
			kgo.RecordHeader{Key: "dlq-reason", Value: []byte(cause.Error())},
			kgo.RecordHeader{Key: "dlq-attempts", Value: []byte(strconv.Itoa(attempts))},
			kgo.RecordHeader{Key: "dlq-source", Value: fmt.Appendf(nil, "%s/%d@%d", r.Topic, r.Partition, r.Offset)},
		),
	}
	// Не смогли записать в DLQ — нельзя и коммитить
	// исходную запись: иначе она потеряна.
	if err := cl.ProduceSync(ctx, d).FirstErr(); err != nil {
		log.Fatal(err)
	}
}
