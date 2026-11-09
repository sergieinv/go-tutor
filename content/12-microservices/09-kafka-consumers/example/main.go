// Группа потребителей: участники делят партиции топика,
// а прогресс — закоммиченные офсеты — хранит брокер. Здесь
// ручной коммит после обработки, повторная доставка после
// сбоя и ребалансировка при входе и выходе участника.

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kfake"
	"github.com/twmb/franz-go/pkg/kgo"
)

const topic = "orders.events"

var seeds []string

func main() {
	ctx := context.Background()
	cluster, err := kfake.NewCluster(kfake.NumBrokers(1), kfake.SeedTopics(3, topic), kfake.SeedTopics(4, "clicks"))
	if err != nil {
		log.Fatal(err)
	}
	defer cluster.Close()
	seeds = cluster.ListenAddrs()
	produce(ctx)

	// Первый запуск «Уведомлений»: событие `B-2 paid`
	// не обрабатывается (например, упал SMTP). Его офсет и
	// всё после него в той же партиции не коммитятся.
	failing := func(r *kgo.Record) error {
		if string(r.Key) == "B-2" && string(r.Value) == "paid" {
			return errors.New("smtp недоступен")
		}
		return nil
	}
	fmt.Println("запуск 1:")
	consume(ctx, "notifications", 6, failing)
	at := committed(ctx, "notifications")
	fmt.Printf("закоммичено: p0=%d p1=%d p2=%d\n", at[0], at[1], at[2])

	// Перезапуск с той же группой: чтение начинается с
	// закоммиченных офсетов. Упавшее событие приходит снова —
	// это at-least-once: потери нет, дубли возможны.
	left := 6 - int(at[0]+at[1]+at[2])
	fmt.Println("запуск 2:")
	consume(ctx, "notifications", left, func(*kgo.Record) error { return nil })

	// Ребалансировка: второй участник группы забирает часть
	// партиций, при его выходе они возвращаются. Топик
	// `clicks` — из четырёх партиций.
	rebalance()
}

func produce(ctx context.Context) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(seeds...))
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	var recs []*kgo.Record
	for _, kv := range []string{"A-1 created", "B-2 created", "A-1 paid", "B-2 paid", "B-2 shipped", "A-1 shipped"} {
		k, v, _ := strings.Cut(kv, " ")
		recs = append(recs, &kgo.Record{Topic: topic, Key: []byte(k), Value: []byte(v)})
	}
	if err := cl.ProduceSync(ctx, recs...).FirstErr(); err != nil {
		log.Fatal(err)
	}
}

// consume читает want записей топика в группе group и
// коммитит только успешно обработанные. Записи партиции
// после сбоя пропускаются: иначе офсет коммита перескочил бы
// через упавшее событие.
func consume(ctx context.Context, group string, want int, handle func(*kgo.Record) error) {
	cl, err := kgo.NewClient(
		kgo.SeedBrokers(seeds...),
		kgo.ConsumerGroup(group),
		kgo.ConsumeTopics(topic),
		kgo.DisableAutoCommit(), // коммитим сами, после обработки
		// Сколько брокер держит fetch, если новых данных нет
		// (по умолчанию 5 с). Короче — чтобы пример не простаивал.
		kgo.FetchMaxWait(200*time.Millisecond),
	)
	if err != nil {
		log.Fatal(err)
	}
	// Close выходит из группы: брокер сразу отдаёт партиции
	// другим участникам, не дожидаясь таймаута сессии.
	defer cl.Close()

	var lines []string
	var done []*kgo.Record
	failed := map[int32]bool{}
	seen := 0
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	for seen < want {
		fetches := cl.PollFetches(ctx)
		if err := fetches.Err(); err != nil {
			log.Fatal(err)
		}
		// Внутри партиции записи идут по порядку офсетов;
		// между партициями порядка нет — печать сортируем.
		fetches.EachRecord(func(r *kgo.Record) {
			seen++
			if failed[r.Partition] {
				lines = append(lines, fmt.Sprintf("p%d@%d %s %s: пропущено", r.Partition, r.Offset, r.Key, r.Value))
				return
			}
			if err := handle(r); err != nil {
				failed[r.Partition] = true
				lines = append(lines, fmt.Sprintf("p%d@%d %s %s: ОШИБКА", r.Partition, r.Offset, r.Key, r.Value))
				return
			}
			done = append(done, r)
			lines = append(lines, fmt.Sprintf("p%d@%d %s %s", r.Partition, r.Offset, r.Key, r.Value))
		})
	}
	// CommitRecords коммитит по каждой партиции офсет
	// последней переданной записи + 1 — «следующую читать
	// отсюда». `WithoutCancel`: коммит нужен и тогда, когда
	// ctx уже отменён остановкой сервиса.
	if err := cl.CommitRecords(context.WithoutCancel(ctx), done...); err != nil {
		log.Fatal(err)
	}
	slices.Sort(lines)
	for _, l := range lines {
		fmt.Println("  " + l)
	}
}

// committed — закоммиченные офсеты группы по партициям.
func committed(ctx context.Context, group string) map[int32]int64 {
	cl, err := kgo.NewClient(kgo.SeedBrokers(seeds...))
	if err != nil {
		log.Fatal(err)
	}
	defer cl.Close()
	offs, err := kadm.NewClient(cl).FetchOffsets(ctx, group)
	if err != nil {
		log.Fatal(err)
	}
	at := map[int32]int64{}
	offs.Each(func(o kadm.OffsetResponse) { at[o.Partition] = o.At })
	return at
}

// rebalance показывает, как участники делят партиции.
// Состав назначений печатаем числом: какие именно номера
// достанутся кому, решает балансировщик.
func rebalance() {
	var mu sync.Mutex
	owned := map[string]int{}
	member := func(name string) *kgo.Client {
		cl, err := kgo.NewClient(
			kgo.SeedBrokers(seeds...),
			kgo.ConsumerGroup("analytics"),
			kgo.ConsumeTopics("clicks"),
			kgo.HeartbeatInterval(100*time.Millisecond), // быстрее узнавать о ребалансировке
			kgo.OnPartitionsAssigned(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
				mu.Lock()
				owned[name] += len(m["clicks"])
				mu.Unlock()
			}),
			kgo.OnPartitionsRevoked(func(_ context.Context, _ *kgo.Client, m map[string][]int32) {
				mu.Lock()
				owned[name] -= len(m["clicks"])
				mu.Unlock()
			}),
		)
		if err != nil {
			log.Fatal(err)
		}
		// Участие в группе — heartbeat и ребалансировки — kgo
		// ведёт в фоне: эти участники даже не вызывают Poll.
		return cl
	}
	waitFor := func(state string) {
		for range 200 {
			mu.Lock()
			s := fmt.Sprintf("first=%d second=%d", owned["first"], owned["second"])
			mu.Unlock()
			if s == state {
				fmt.Println("партиций:", s)
				return
			}
			time.Sleep(25 * time.Millisecond)
		}
		log.Fatalf("не дождались %s", state)
	}

	first := member("first")
	defer first.Close()
	waitFor("first=4 second=0")
	second := member("second")
	waitFor("first=2 second=2")
	second.Close()
	waitFor("first=4 second=0")
}
