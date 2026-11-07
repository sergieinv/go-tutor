package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"
)

var (
	// ErrTooLarge — тело ответа длиннее лимита.
	ErrTooLarge = errors.New("getjson: response too large")
	// ErrNotJSON — Content-Type ответа не application/json.
	ErrNotJSON = errors.New("getjson: response is not JSON")
)

// StatusError — ответ со статусом вне диапазона 200–299.
type StatusError struct{ StatusCode int }

func (e *StatusError) Error() string {
	return fmt.Sprintf("getjson: unexpected status %d", e.StatusCode)
}

// GetJSON выполняет GET-запрос и разбирает JSON-ответ в v.
func GetJSON(ctx context.Context, client *http.Client, url string, maxBytes int64, v any) error {
	// TODO: реализуйте функцию
	return nil
}
