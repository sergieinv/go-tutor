package task

import (
	"context"

	"golang.org/x/sync/errgroup"
)

// ProcessAll параллельно применяет f к items с помощью errgroup.
// Порядок результатов совпадает с items; при первой ошибке
// остальные вызовы отменяются, а результат — nil.
func ProcessAll(ctx context.Context, items []string, limit int,
	f func(ctx context.Context, item string) (int, error)) ([]int, error) {
	g, ctx := errgroup.WithContext(ctx)
	// SetLimit(0) заблокировал бы Go навсегда: лимит ставим только для limit > 0.
	if limit > 0 {
		g.SetLimit(limit)
	}

	out := make([]int, len(items))
	for i, item := range items {
		g.Go(func() error {
			// Очередь дошла до элемента уже после ошибки — не начинаем.
			if err := ctx.Err(); err != nil {
				return err
			}
			v, err := f(ctx, item)
			out[i] = v // свой элемент — без гонок
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return out, nil
}
