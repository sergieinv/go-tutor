package task

import (
	"context"
	"log/slog"
)

// WithAttrs возвращает контекст с добавленными атрибутами запроса.
func WithAttrs(ctx context.Context, attrs ...slog.Attr) context.Context {
	// TODO
	return ctx
}

// AttrsFrom возвращает копию атрибутов запроса из контекста.
func AttrsFrom(ctx context.Context) []slog.Attr {
	// TODO
	return nil
}

// NewContextHandler оборачивает next.
func NewContextHandler(next slog.Handler) slog.Handler {
	// TODO: верните свою обёртку
	return next
}
