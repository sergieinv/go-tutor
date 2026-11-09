// Kafka — распределённый лог: продюсер дописывает записи
// в конец партиции, каждая запись получает офсет. В песочнице
// брокера нет, поэтому кластер поднимает `kfake` прямо в
// процессе; клиент `kgo` работает с ним так же, как с
// настоящей Kafka.

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"sync"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

const topic = "orders.events"

// OrderEvent — тело сообщения. Формат — JSON; protobuf
// из модуля 11 подключается так же: `proto.Marshal` в
// `Value`, тип сообщения — в заголовке.
type OrderEvent struct {
	OrderID string `json:"order_id"`
	Type    string `json:"type"`
}

func record(e OrderEvent) *kgo.Record {
	value, _ := json.Marshal(e)
	return &kgo.Record{
		Topic: topic,
		// Ключ выбирает партицию: hash(key) % число партиций.
		// Все события заказа попадут в одну партицию и
		// сохранят порядок.
		Key:   []byte(e.OrderID),
		Value: value,
		// Заголовки — метаданные рядом с телом: тип события,
		// формат, id трассы (урок 3).
		Headers: []kgo.RecordHeader{
			{Key: "event-type", Value: []byte(e.Type)},
			{Key: "content-type", Value: []byte("application/json")},
		},
	}
}

func main() {
	ctx := context.Background()
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1))
	if err != nil {
		log.Fatal(err)
	}
	defer cluster.Close()

	// Клиент один на процесс: он и продюсер, и консьюмер,
	// и держит соединения со всеми брокерами.
	cl, err := kgo.NewClient(kgo.SeedBrokers(cluster.ListenAddrs()...))
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()

	// Топики создаёт админ-клиент `kadm`: 3 партиции,
	// фактор репликации 1 (в проде — 3), хранить неделю.
	// Автосоздание топиков в проде обычно выключено.
	adm := kadm.NewClient(cl)
	week := "604800000"
	t, err := adm.CreateTopic(ctx, 3, 1, map[string]*string{"retention.ms": &week}, topic)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("топик:", t.Topic, "партиций:", t.NumPartitions)

	// `ProduceSync` ждёт подтверждения брокера и возвращает
	// партицию и офсет записи. Удобно, но каждая запись —
	// сетевой круг.
	for _, e := range []OrderEvent{{"A-1", "created"}, {"B-2", "created"}, {"A-1", "paid"}} {
		r, err := cl.ProduceSync(ctx, record(e)).First()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("sync  %s %-9s → p%d@%d\n", e.OrderID, e.Type, r.Partition, r.Offset)
	}

	// `Produce` асинхронный: запись уходит в буфер, клиент
	// собирает пачки, колбэк вызывается по подтверждении.
	// Колбэки идут из горутин клиента — общие данные под
	// мьютексом; `Flush` ждёт все отправленные записи.
	var (
		mu   sync.Mutex
		acks []string
	)
	for _, e := range []OrderEvent{{"C-3", "created"}, {"A-1", "shipped"}, {"B-2", "cancelled"}} {
		cl.Produce(ctx, record(e), func(r *kgo.Record, err error) {
			mu.Lock()
			defer mu.Unlock()
			if err != nil {
				acks = append(acks, "ошибка: "+err.Error())
				return
			}
			acks = append(acks, fmt.Sprintf("async %s %-9s → p%d@%d", r.Key, e.Type, r.Partition, r.Offset))
		})
	}
	if err := cl.Flush(ctx); err != nil {
		log.Fatal(err)
	}
	slices.Sort(acks) // порядок колбэков разных партиций не задан
	for _, a := range acks {
		fmt.Println(a)
	}

	// Читаем топик с начала отдельным клиентом без группы
	// (группы — урок 9). Порядок есть только внутри
	// партиции, поэтому печатаем по партициям.
	reader, err := kgo.NewClient(
		kgo.SeedBrokers(cluster.ListenAddrs()...),
		kgo.ConsumeTopics(topic),
		kgo.ConsumeResetOffset(kgo.NewOffset().AtStart()),
	)
	if err != nil {
		log.Fatal(err)
	}
	defer reader.Close()
	byPartition := map[int32][]string{}
	for got := 0; got < 6; {
		fetches := reader.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			log.Fatal(err)
		}
		fetches.EachRecord(func(r *kgo.Record) {
			var e OrderEvent
			if err := json.Unmarshal(r.Value, &e); err != nil {
				log.Fatal(err)
			}
			byPartition[r.Partition] = append(byPartition[r.Partition],
				fmt.Sprintf("%d:%s/%s", r.Offset, e.OrderID, header(r, "event-type")))
			got++
		})
	}
	for p := range int32(3) {
		fmt.Printf("партиция %d: %v\n", p, byPartition[p])
	}
}

func header(r *kgo.Record, key string) string {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
