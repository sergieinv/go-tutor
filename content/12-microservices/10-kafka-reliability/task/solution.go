package task

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/twmb/franz-go/pkg/kgo"
)

const (
	HeaderEventID      = "event-id"
	HeaderDLQReason    = "dlq-reason"
	HeaderDLQAttempts  = "dlq-attempts"
	HeaderDLQTopic     = "dlq-original-topic"
	HeaderDLQPartition = "dlq-original-partition"
	HeaderDLQOffset    = "dlq-original-offset"
)

var ErrNoEventID = errors.New("reliability: нет заголовка event-id")

// Outcome — чем закончилась обработка записи.
type Outcome int

const (
	Processed    Outcome = iota + 1 // обработано
	Duplicate                       // уже обрабатывали — пропущено
	DeadLettered                    // отправлено в DLQ
)

type permanentError struct{ err error }

func (e permanentError) Error() string { return e.err.Error() }
func (e permanentError) Unwrap() error { return e.err }

// Permanent помечает ошибку, которую повторять бесполезно.
func Permanent(err error) error { return permanentError{err} }

// IsPermanent сообщает, помечена ли ошибка (или любая в цепочке) как постоянная.
func IsPermanent(err error) bool {
	var p permanentError
	return errors.As(err, &p)
}

// Producer — то, что нужно для записи в DLQ; *kgo.Client подходит.
type Producer interface {
	ProduceSync(ctx context.Context, rs ...*kgo.Record) kgo.ProduceResults
}

// Store — таблица обработанных ключей идемпотентности.
type Store interface {
	Seen(ctx context.Context, id string) (bool, error)
	MarkSeen(ctx context.Context, id string) error
}

type Processor struct {
	Handle      func(ctx context.Context, r *kgo.Record) error
	DLQ         Producer
	DLQTopic    string
	MaxAttempts int // <= 0 — 3
	Store       Store
}

// Process обрабатывает запись: повторы, DLQ, дедупликация по event-id.
func (p *Processor) Process(ctx context.Context, r *kgo.Record) (Outcome, error) {
	id := header(r, HeaderEventID)
	if id == "" {
		// Без ключа идемпотентности обработать безопасно нельзя.
		if err := p.toDLQ(ctx, r, ErrNoEventID, 0); err != nil {
			return 0, err
		}
		return DeadLettered, nil
	}
	seen, err := p.Store.Seen(ctx, id)
	if err != nil {
		return 0, fmt.Errorf("reliability: %s: %w", id, err)
	}
	if seen {
		return Duplicate, nil
	}

	maxAttempts := p.MaxAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}
	attempts := 0
	for attempts < maxAttempts {
		if attempts > 0 && ctx.Err() != nil {
			return 0, fmt.Errorf("reliability: %s: остановка: %w", id, ctx.Err())
		}
		attempts++
		if err = p.Handle(ctx, r); err == nil || IsPermanent(err) {
			break
		}
	}
	if err == nil {
		if err := p.Store.MarkSeen(ctx, id); err != nil {
			return 0, fmt.Errorf("reliability: %s: %w", id, err)
		}
		return Processed, nil
	}
	if err := p.toDLQ(ctx, r, err, attempts); err != nil {
		return 0, err
	}
	// Отмечаем и отправленное в DLQ: повторная доставка не должна
	// отправить его туда второй раз.
	if err := p.Store.MarkSeen(ctx, id); err != nil {
		return 0, fmt.Errorf("reliability: %s: %w", id, err)
	}
	return DeadLettered, nil
}

func (p *Processor) toDLQ(ctx context.Context, r *kgo.Record, cause error, attempts int) error {
	// Новый срез: append к r.Headers мог бы записать в его массив.
	headers := make([]kgo.RecordHeader, 0, len(r.Headers)+5)
	headers = append(headers, r.Headers...)
	headers = append(headers,
		kgo.RecordHeader{Key: HeaderDLQReason, Value: []byte(cause.Error())},
		kgo.RecordHeader{Key: HeaderDLQAttempts, Value: []byte(strconv.Itoa(attempts))},
		kgo.RecordHeader{Key: HeaderDLQTopic, Value: []byte(r.Topic)},
		kgo.RecordHeader{Key: HeaderDLQPartition, Value: []byte(strconv.Itoa(int(r.Partition)))},
		kgo.RecordHeader{Key: HeaderDLQOffset, Value: []byte(strconv.FormatInt(r.Offset, 10))},
	)
	d := &kgo.Record{Topic: p.DLQTopic, Key: r.Key, Value: r.Value, Headers: headers}
	if err := p.DLQ.ProduceSync(ctx, d).FirstErr(); err != nil {
		return fmt.Errorf("reliability: DLQ: %w", err)
	}
	return nil
}

func header(r *kgo.Record, key string) string {
	for _, h := range r.Headers {
		if h.Key == key {
			return string(h.Value)
		}
	}
	return ""
}
