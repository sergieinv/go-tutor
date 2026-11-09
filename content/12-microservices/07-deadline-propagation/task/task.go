package task

import (
	"context"
	"errors"
	"time"
)

// ErrNoTime — соседу не хватит времени: звать его бессмысленно.
var ErrNoTime = errors.New("not enough time for the call")

// Budget — как делить остаток времени запроса с соседом.
type Budget struct {
	Reserve time.Duration
	Max     time.Duration
	Min     time.Duration
	Now     func() time.Time
}

// Child возвращает контекст для вызова соседа.
func (b Budget) Child(ctx context.Context) (context.Context, context.CancelFunc, error) {
	// TODO
	return ctx, func() {}, nil
}

// Gather вызывает соседей параллельно с общим дочерним контекстом.
func Gather(ctx context.Context, b Budget, calls ...func(ctx context.Context) error) error {
	// TODO
	return nil
}
