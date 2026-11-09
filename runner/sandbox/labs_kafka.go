package sandbox

import (
	"context"
	"strings"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kgo"
)

// Kafka профиля labs общая на все запуски: отдельного кластера на запуск
// нет, поэтому программа получает префикс KAFKA_TOPIC_PREFIX и обязана
// называть им свои топики и группы потребителей. После запуска runner
// удаляет всё с этим префиксом. Автосоздание топиков в labs выключено.

// createKafka проверяет, что брокер отвечает, и выдаёт переменные окружения.
func createKafka(ctx context.Context, brokers, id string) ([]string, error) {
	adm, cl, err := kafkaAdmin(brokers)
	if err != nil {
		return nil, err
	}
	defer cl.Close()
	if _, err := adm.ListBrokers(ctx); err != nil {
		return nil, err
	}
	return []string{"KAFKA_BROKERS=" + brokers, "KAFKA_TOPIC_PREFIX=" + id + "."}, nil
}

// dropKafka удаляет топики и группы потребителей, чей префикс run_… принят owned.
func dropKafka(ctx context.Context, brokers string, owned func(id string) bool) error {
	adm, cl, err := kafkaAdmin(brokers)
	if err != nil {
		return err
	}
	defer cl.Close()
	owner := func(name string) bool {
		id, _, ok := strings.Cut(name, ".")
		return ok && validLabID(id) && owned(id)
	}
	topics, err := adm.ListTopics(ctx)
	if err != nil {
		return err
	}
	var drop []string
	for _, t := range topics.Names() {
		if owner(t) {
			drop = append(drop, t)
		}
	}
	if len(drop) > 0 {
		adm.DeleteTopics(ctx, drop...)
	}
	groups, err := adm.ListGroups(ctx)
	if err != nil {
		return err
	}
	var gdrop []string
	for _, g := range groups.Groups() {
		if owner(g) {
			gdrop = append(gdrop, g)
		}
	}
	if len(gdrop) > 0 {
		adm.DeleteGroups(ctx, gdrop...)
	}
	return nil
}

func kafkaAdmin(brokers string) (*kadm.Client, *kgo.Client, error) {
	cl, err := kgo.NewClient(kgo.SeedBrokers(strings.Split(brokers, ",")...))
	if err != nil {
		return nil, nil, err
	}
	return kadm.NewClient(cl), cl, nil
}
