package task

import (
	"context"
	"errors"

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
	// TODO
	return 0, errors.New("не реализовано")
}
