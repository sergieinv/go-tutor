// Package kafkax — мелочи для работы с Kafka через franz-go, общие для
// продюсера «Заказов» и консьюмера «Уведомлений»: заголовки записи как
// носитель контекста трассировки и создание топика при старте.
package kafkax

import (
	"context"
	"errors"
	"fmt"

	"github.com/twmb/franz-go/pkg/kadm"
	"github.com/twmb/franz-go/pkg/kerr"
	"github.com/twmb/franz-go/pkg/kgo"
	"go.opentelemetry.io/otel/propagation"
)

// HeaderCarrier адаптирует заголовки записи Kafka к propagation.TextMapCarrier:
// продюсер кладёт в них traceparent, консьюмер достаёт — и трасса
// продолжается через брокер так же, как через gRPC-метаданные.
type HeaderCarrier struct {
	r *kgo.Record
}

// NewHeaderCarrier возвращает носитель поверх заголовков r.
func NewHeaderCarrier(r *kgo.Record) HeaderCarrier { return HeaderCarrier{r: r} }

var _ propagation.TextMapCarrier = HeaderCarrier{}

// Get возвращает значение первого заголовка с ключом key или "".
func (c HeaderCarrier) Get(key string) string { return Header(c.r, key) }

// Set заменяет значение заголовка key (или добавляет заголовок).
func (c HeaderCarrier) Set(key, value string) { SetHeader(c.r, key, value) }

// Keys возвращает ключи всех заголовков.
func (c HeaderCarrier) Keys() []string {
	keys := make([]string, len(c.r.Headers))
	for i, h := range c.r.Headers {
		keys[i] = h.Key
	}
	return keys
}

// Header возвращает значение первого заголовка с ключом key или "".
// Ключи заголовков Kafka чувствительны к регистру.
func Header(r *kgo.Record, key string) string {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}

// SetHeader заменяет значение заголовка key или добавляет его.
func SetHeader(r *kgo.Record, key, value string) {
	for i := range r.Headers {
		if r.Headers[i].Key == key {
			r.Headers[i].Value = []byte(value)
			return
		}
	}
	r.Headers = append(r.Headers, kgo.RecordHeader{Key: key, Value: []byte(value)})
}

// EnsureTopic создаёт топик, если его нет. Автосоздание топиков у брокера
// выключено (docker-compose.yml): топик с одной партицией и настройками по
// умолчанию, созданный первым же продюсером по опечатке, — частая авария.
// Поэтому число партиций и фактор репликации задаёт владелец топика явно.
func EnsureTopic(ctx context.Context, cl *kgo.Client, topic string, partitions int32, replicationFactor int16) error {
	_, err := kadm.NewClient(cl).CreateTopic(ctx, partitions, replicationFactor, nil, topic)
	if err != nil && !errors.Is(err, kerr.TopicAlreadyExists) {
		return fmt.Errorf("create topic %s: %w", topic, err)
	}
	return nil
}
