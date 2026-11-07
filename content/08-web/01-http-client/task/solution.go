package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"time"
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
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return fmt.Errorf("getjson: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("getjson: %w", err)
	}
	defer resp.Body.Close()

	// Читаем не больше maxBytes+1: лишний байт говорит о превышении.
	// Тело, уложившееся в лимит, прочитано до EOF — соединение
	// вернётся в пул при любом исходе проверок ниже.
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return fmt.Errorf("getjson: %w", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{StatusCode: resp.StatusCode}
	}
	mt, _, err := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	if err != nil || mt != "application/json" {
		return ErrNotJSON
	}
	if int64(len(body)) > maxBytes {
		return ErrTooLarge
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("getjson: %w", err)
	}
	return nil
}
