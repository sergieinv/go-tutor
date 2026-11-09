package task

import (
	"context"
	"errors"
	"net/http"
	"time"
)

// MaxBody — сколько байт тела ответа клиент готов прочитать.
const MaxBody = 1 << 20

// ErrTooLarge — ответ больше MaxBody по Content-Length.
var ErrTooLarge = errors.New("response body too large")

// StatusError — ответ со статусом вне 200–299.
type StatusError struct{ Code int }

func (e *StatusError) Error() string {
	// TODO: "unexpected status <код>"
	return ""
}

// NewClient создаёт клиент со своим настроенным пулом соединений.
func NewClient(maxConnsPerHost int, timeout time.Duration) *http.Client {
	// TODO: свой *http.Transport с лимитами и таймаутами
	return &http.Client{}
}

// GetJSON делает GET и декодирует JSON-ответ в dst так, чтобы
// соединение всегда возвращалось в пул.
func GetJSON(ctx context.Context, c *http.Client, url string, dst any) error {
	// TODO: запрос с ctx, проверка статуса и размера, Decode, дочитывание и Close
	return nil
}
