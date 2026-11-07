package task

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// ErrNotFound — Fetcher не нашёл запись.
var ErrNotFound = errors.New("not found")

// Fetcher загружает запись по id (база, другой сервис).
type Fetcher func(ctx context.Context, id string) (string, error)

// Handler отдаёт запись ?id=... с собственным лимитом времени.
func Handler(fetch Fetcher, timeout time.Duration) http.Handler {
	// TODO: реализуйте функцию
	return http.NotFoundHandler()
}
