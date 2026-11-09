package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// MaxBody — сколько байт тела ответа клиент готов прочитать.
const MaxBody = 1 << 20

// ErrTooLarge — ответ больше MaxBody по Content-Length.
var ErrTooLarge = errors.New("response body too large")

// StatusError — ответ со статусом вне 200–299.
type StatusError struct{ Code int }

func (e *StatusError) Error() string { return fmt.Sprintf("unexpected status %d", e.Code) }

// NewClient создаёт клиент со своим пулом соединений: простаивающих
// на хост держим столько же, сколько разрешаем всего, — тогда после
// пика соединения не закрываются и не открываются заново.
func NewClient(maxConnsPerHost int, timeout time.Duration) *http.Client {
	n := max(maxConnsPerHost, 1)
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	dialer := &net.Dialer{Timeout: 3 * time.Second, KeepAlive: 30 * time.Second}
	tr := http.DefaultTransport.(*http.Transport).Clone()
	tr.DialContext = dialer.DialContext
	tr.MaxConnsPerHost = n
	tr.MaxIdleConnsPerHost = n
	tr.MaxIdleConns = max(tr.MaxIdleConns, n)
	tr.IdleConnTimeout = 90 * time.Second
	tr.TLSHandshakeTimeout = 5 * time.Second
	return &http.Client{Transport: tr, Timeout: timeout}
}

// GetJSON делает GET и декодирует JSON. Тело всегда дочитывается
// (с лимитом) и закрывается — соединение возвращается в пул.
func GetJSON(ctx context.Context, c *http.Client, url string, dst any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("get %s: %w", url, err)
	}
	if resp.ContentLength > MaxBody {
		resp.Body.Close() // дочитывать мегабайты ради соединения не стоит
		return ErrTooLarge
	}
	defer func() {
		io.Copy(io.Discard, io.LimitReader(resp.Body, MaxBody))
		resp.Body.Close()
	}()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Code: resp.StatusCode}
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, MaxBody)).Decode(dst); err != nil {
		return fmt.Errorf("decode %s: %w", url, err)
	}
	return nil
}
