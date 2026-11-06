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

// Search опрашивает все бэкенды одновременно и возвращает
// первый успешный ответ.
func Search(ctx context.Context, timeout time.Duration, backends ...Backend) (string, error) {
	// TODO: реализуйте функцию
	return "", nil
}
