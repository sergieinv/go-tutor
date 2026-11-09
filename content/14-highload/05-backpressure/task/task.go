package task

import (
	"context"
	"errors"
	"time"
)

var (
	ErrOverloaded   = errors.New("overloaded: queue is full")
	ErrQueueTimeout = errors.New("overloaded: queue wait timeout")
)

// Config — настройки ограничителя (см. «Задание»).
type Config struct {
	MaxConcurrent int                                  // N: сколько fn выполняется одновременно
	MaxQueue      int                                  // M: сколько вызовов может ждать места
	QueueTimeout  time.Duration                        // T: сколько вызов ждёт в очереди
	After         func(time.Duration) <-chan time.Time // таймер очереди; nil — time.After
}

// Stats — текущее состояние и счётчики ограничителя.
type Stats struct {
	InFlight int   // fn выполняется сейчас
	Queued   int   // ждут места сейчас
	Accepted int64 // всего вызовов, получивших место (fn вызвана)
	Rejected int64 // всего отказов сразу: очередь полна
	TimedOut int64 // всего ушли из очереди по таймауту
	Canceled int64 // всего отменены контекстом (до вызова или в очереди)
}

type Limiter struct {
	// TODO: состояние ограничителя
}

func NewLimiter(cfg Config) *Limiter {
	// TODO
	return &Limiter{}
}

func (l *Limiter) Do(ctx context.Context, fn func(context.Context) error) error {
	// TODO: выполнить сразу, поставить в очередь или отказать
	return nil
}

func (l *Limiter) Stats() Stats {
	// TODO
	return Stats{}
}
