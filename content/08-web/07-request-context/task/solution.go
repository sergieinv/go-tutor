package task

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// ErrNotFound — Fetcher не нашёл запись.
var ErrNotFound = errors.New("not found")

// Fetcher загружает запись по id (база, другой сервис).
type Fetcher func(ctx context.Context, id string) (string, error)

type result struct {
	value string
	err   error
}

// Handler отдаёт запись ?id=... с собственным лимитом времени.
func Handler(fetch Fetcher, timeout time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("id")
		if id == "" {
			http.Error(w, "missing id", http.StatusBadRequest)
			return
		}

		// Производный контекст: значения запроса и его отмена
		// доходят до fetch, плюс собственный лимит.
		var ctx context.Context
		var cancel context.CancelFunc
		if timeout > 0 {
			ctx, cancel = context.WithTimeout(r.Context(), timeout)
		} else {
			ctx, cancel = context.WithCancel(r.Context())
		}
		defer cancel()

		// Буфер 1: опоздавший fetch отправит результат и завершится,
		// даже если хендлер уже ответил.
		done := make(chan result, 1)
		go func() {
			v, err := fetch(ctx, id)
			done <- result{v, err}
		}()

		var res result
		select {
		case res = <-done:
		case <-ctx.Done():
			res.err = ctx.Err()
		}

		switch {
		case res.err == nil:
			io.WriteString(w, res.value)
		case r.Context().Err() != nil:
			http.Error(w, "request canceled", http.StatusServiceUnavailable)
		case ctx.Err() != nil:
			http.Error(w, "timeout", http.StatusGatewayTimeout)
		case errors.Is(res.err, ErrNotFound):
			http.Error(w, "not found", http.StatusNotFound)
		default:
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
	})
}
