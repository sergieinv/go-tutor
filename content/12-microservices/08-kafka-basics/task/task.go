package task

import (
	"context"
	"errors"

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
	// TODO
	return nil, errors.New("не реализовано")
}

// Publish синхронно отправляет события и возвращает их позиции
// в порядке events.
func Publish(ctx context.Context, cl *kgo.Client, topic string, events []OrderEvent) ([]Position, error) {
	// TODO
	return nil, errors.New("не реализовано")
}

// Histories восстанавливает историю (типы событий по порядку)
// каждого заказа из прочитанных записей.
func Histories(records []*kgo.Record) (map[string][]string, error) {
	// TODO
	return nil, errors.New("не реализовано")
}
