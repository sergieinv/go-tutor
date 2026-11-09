package task

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	HeaderEventType   = "event-type"
	HeaderContentType = "content-type"
	ContentTypeJSON   = "application/json"
)

var (
	ErrNoOrderID = errors.New("events: пустой order id")
	ErrSplitKey  = errors.New("events: ключ в нескольких партициях")
)

// OrderEvent — событие заказа, тело записи в JSON.
type OrderEvent struct {
	OrderID string `json:"order_id"`
	Type    string `json:"type"`
	Amount  int64  `json:"amount"`
}

// Position — куда брокер записал событие.
type Position struct {
	Partition int32
	Offset    int64
}

// NewRecord строит запись Kafka для события: ключ — id заказа.
func NewRecord(topic string, e OrderEvent) (*kgo.Record, error) {
	if e.OrderID == "" {
		return nil, ErrNoOrderID
	}
	value, err := json.Marshal(e)
	if err != nil {
		return nil, fmt.Errorf("events: %w", err)
	}
	return &kgo.Record{
		Topic: topic,
		Key:   []byte(e.OrderID),
		Value: value,
		Headers: []kgo.RecordHeader{
			{Key: HeaderEventType, Value: []byte(e.Type)},
			{Key: HeaderContentType, Value: []byte(ContentTypeJSON)},
		},
	}, nil
}

// Publish синхронно отправляет события и возвращает их позиции
// в порядке events.
func Publish(ctx context.Context, cl *kgo.Client, topic string, events []OrderEvent) ([]Position, error) {
	// Сначала все записи: ошибка в любом событии — ничего не отправлено.
	recs := make([]*kgo.Record, 0, len(events))
	for i, e := range events {
		r, err := NewRecord(topic, e)
		if err != nil {
			return nil, fmt.Errorf("событие %d: %w", i, err)
		}
		recs = append(recs, r)
	}
	if len(recs) == 0 {
		return []Position{}, nil
	}
	// Результаты ProduceSync идут в порядке подтверждений, а позицию
	// клиент записывает в сами записи — по ним и собираем ответ.
	if err := cl.ProduceSync(ctx, recs...).FirstErr(); err != nil {
		return nil, fmt.Errorf("events: отправка: %w", err)
	}
	out := make([]Position, len(recs))
	for i, r := range recs {
		out[i] = Position{Partition: r.Partition, Offset: r.Offset}
	}
	return out, nil
}

// Histories восстанавливает историю (типы событий по порядку)
// каждого заказа из прочитанных записей.
func Histories(records []*kgo.Record) (map[string][]string, error) {
	sorted := slices.Clone(records)
	slices.SortStableFunc(sorted, func(a, b *kgo.Record) int {
		return cmp.Compare(a.Offset, b.Offset)
	})
	partition := map[string]int32{}
	out := map[string][]string{}
	for _, r := range sorted {
		key := string(r.Key)
		if key == "" {
			return nil, fmt.Errorf("офсет %d: %w", r.Offset, ErrNoOrderID)
		}
		if p, ok := partition[key]; ok && p != r.Partition {
			return nil, fmt.Errorf("%s: партиции %d и %d: %w", key, p, r.Partition, ErrSplitKey)
		}
		partition[key] = r.Partition
		var e OrderEvent
		if err := json.Unmarshal(r.Value, &e); err != nil {
			return nil, fmt.Errorf("%s@%d: %w", key, r.Offset, err)
		}
		out[key] = append(out[key], e.Type)
	}
	return out, nil
}
