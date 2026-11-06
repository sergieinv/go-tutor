package task

import (
	"context"
	"errors"
	"time"
)

// Backend — источник ответа, который уважает отмену ctx.
type Backend func(ctx context.Context) (string, error)

// ErrNoBackends — Search вызван без бэкендов.
var ErrNoBackends = errors.New("no backends")

type reply struct {
	value string
	err   error
}

// Search опрашивает все бэкенды одновременно и возвращает
// первый успешный ответ.
func Search(ctx context.Context, timeout time.Duration, backends ...Backend) (string, error) {
	if len(backends) == 0 {
		return "", ErrNoBackends
	}

	// WithTimeout(ctx, 0) истёк бы сразу — лимит только при timeout > 0.
	var cancel context.CancelFunc
	if timeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, timeout)
	} else {
		ctx, cancel = context.WithCancel(ctx)
	}
	// При любом выходе отменяем остальные бэкенды.
	defer cancel()

	// Буфер на всех: опоздавший бэкенд отправит ответ
	// и завершится, даже если его уже никто не ждёт.
	replies := make(chan reply, len(backends))
	for _, b := range backends {
		go func() {
			v, err := b(ctx)
			replies <- reply{v, err}
		}()
	}

	var errs []error
	for range backends {
		select {
		case r := <-replies:
			if r.err == nil {
				return r.value, nil
			}
			errs = append(errs, r.err)
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	// Бэкенды, уважающие отмену, отвечают ошибкой почти одновременно
	// с ctx.Done(), и select выбрал бы ветку случайно. Приоритет —
	// у ошибки контекста.
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return "", errors.Join(errs...)
}
