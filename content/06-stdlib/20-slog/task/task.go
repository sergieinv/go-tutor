package task

import (
	"context"
	"log/slog"
)

// WithRequestID возвращает контекст с идентификатором запроса.
func WithRequestID(ctx context.Context, id string) context.Context {
	// TODO: реализуйте функцию
	return ctx
}

// RequestID достаёт идентификатор запроса или возвращает "".
func RequestID(ctx context.Context) string {
	// TODO: реализуйте функцию
	return ""
}

// NewContextHandler оборачивает next.
func NewContextHandler(next slog.Handler) slog.Handler {
	// TODO: верните обёртку, которая добавляет request_id
	return next
}
