package task

import (
	"context"
	"log/slog"
)

// requestIDKey — неэкспортируемый тип ключа: чужой пакет не перезапишет значение.
type requestIDKey struct{}

// WithRequestID возвращает контекст с идентификатором запроса.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID достаёт идентификатор запроса или возвращает "".
func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// contextHandler добавляет request_id из контекста к каждой записи.
type contextHandler struct {
	next slog.Handler
}

// NewContextHandler оборачивает next.
func NewContextHandler(next slog.Handler) slog.Handler {
	return &contextHandler{next: next}
}

// Enabled: уровень отсечения решает внутренний обработчик.
func (h *contextHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *contextHandler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r = r.Clone() // не делим срез атрибутов с вызывающим
		r.AddAttrs(slog.String("request_id", id))
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs и WithGroup возвращают новую обёртку, иначе после
// logger.With(...) request_id перестал бы добавляться.
func (h *contextHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &contextHandler{next: h.next.WithAttrs(attrs)}
}

func (h *contextHandler) WithGroup(name string) slog.Handler {
	return &contextHandler{next: h.next.WithGroup(name)}
}
