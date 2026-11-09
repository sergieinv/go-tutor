package task

import (
	"context"
	"errors"
	"sync"
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

// Child возвращает контекст для вызова соседа: дедлайн — остаток минус
// запас, не больше Max; меньше Min — ErrNoTime.
func (b Budget) Child(ctx context.Context) (context.Context, context.CancelFunc, error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	now := time.Now
	if b.Now != nil {
		now = b.Now
	}
	t := now()
	timeout := b.Max
	if dl, ok := ctx.Deadline(); ok {
		timeout = min(dl.Sub(t)-b.Reserve, b.Max)
	}
	if timeout < b.Min {
		return nil, nil, ErrNoTime
	}
	child, cancel := context.WithDeadline(ctx, t.Add(timeout))
	return child, cancel, nil
}

// Gather вызывает соседей параллельно с общим дочерним контекстом:
// первая ошибка отменяет остальных, возврат — после завершения всех.
func Gather(ctx context.Context, b Budget, calls ...func(ctx context.Context) error) error {
	if len(calls) == 0 {
		return nil
	}
	child, cancel, err := b.Child(ctx)
	if err != nil {
		return err
	}
	defer cancel()
	child, stop := context.WithCancel(child)
	defer stop()

	var (
		wg    sync.WaitGroup
		once  sync.Once
		first error
	)
	for _, call := range calls {
		wg.Go(func() {
			if err := call(child); err != nil {
				once.Do(func() {
					first = err
					stop()
				})
			}
		})
	}
	wg.Wait()
	return first
}
