// Package logging — структурный журнал сервисов на log/slog: JSON в stdout,
// request_id и trace_id в каждой записи, сделанной с контекстом запроса.
//
// В .NET то же дают ILogger со scope'ами и Serilog-энричеры. В Go scope'ов
// нет: атрибуты запроса едут в context.Context, а обёртка над slog.Handler
// достаёт их оттуда в момент записи. Поэтому журналировать нужно методами
// с контекстом — log.InfoContext(ctx, …), а не log.Info(…).
package logging

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"log/slog"

	"go.opentelemetry.io/otel/trace"
)

// New создаёт журнал сервиса: JSON-записи в w, уровень level, у каждой
// записи — атрибут service.
func New(w io.Writer, level slog.Leveler, service string) *slog.Logger {
	json := slog.NewJSONHandler(w, &slog.HandlerOptions{Level: level})
	return slog.New(NewHandler(json)).With("service", service)
}

// ParseLevel разбирает уровень из конфигурации: debug, info, warn, error.
// Пустая строка — info.
func ParseLevel(s string) (slog.Level, error) {
	if s == "" {
		return slog.LevelInfo, nil
	}
	var l slog.Level
	if err := l.UnmarshalText([]byte(s)); err != nil {
		return 0, fmt.Errorf("log level %q: want debug, info, warn or error", s)
	}
	return l, nil
}

// Handler добавляет к записи атрибуты из контекста: request_id (см.
// WithRequestID) и trace_id/span_id текущего спана OpenTelemetry. По
// trace_id запись журнала находится рядом с трассой в Jaeger.
type Handler struct {
	next slog.Handler
}

// NewHandler оборачивает next.
func NewHandler(next slog.Handler) *Handler { return &Handler{next: next} }

// Enabled делегирует решение об уровне обёрнутому обработчику.
func (h *Handler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle дописывает атрибуты контекста и передаёт запись дальше.
//
// Запись (slog.Record) передаётся по значению, но делит память атрибутов
// с вызывающим. AddAttrs на копии безопасен: slog копирует атрибуты при
// необходимости сам. Если же журнал создан с WithGroup, атрибуты попадут
// внутрь группы — таков контракт slog.
func (h *Handler) Handle(ctx context.Context, r slog.Record) error {
	if id := RequestID(ctx); id != "" {
		r.AddAttrs(slog.String("request_id", id))
	}
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs сохраняет обёртку у производного обработчика.
func (h *Handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &Handler{next: h.next.WithAttrs(attrs)}
}

// WithGroup сохраняет обёртку у производного обработчика.
func (h *Handler) WithGroup(name string) slog.Handler {
	return &Handler{next: h.next.WithGroup(name)}
}

type requestIDKey struct{}

// WithRequestID кладёт id запроса в контекст.
func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, id)
}

// RequestID возвращает id запроса из контекста или "".
func RequestID(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)
	return id
}

// NewRequestID возвращает случайный id запроса: 26 символов base32,
// 128 бит из crypto/rand.
func NewRequestID() string { return rand.Text() }

// ValidRequestID сообщает, можно ли принять id, пришедший извне (заголовок
// X-Request-ID, metadata x-request-id). Чужой id попадает в журнал как есть,
// поэтому — только короткий и из безопасных символов: никаких переводов
// строк и управляющих символов, которыми подделывают записи журнала.
func ValidRequestID(id string) bool {
	if len(id) == 0 || len(id) > 64 {
		return false
	}
	for i := 0; i < len(id); i++ {
		c := id[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}
	return true
}

// Secret — строка, которая не попадает в журнал и в fmt-вывод: пароль,
// DSN с паролем, токен. Значение достаётся явно — Reveal().
//
//	log.Info("config", "dsn", cfg.DSN) // → "dsn":"[REDACTED]"
type Secret string

// Reveal возвращает настоящее значение — только для передачи драйверу.
func (s Secret) Reveal() string { return string(s) }

// LogValue скрывает значение в slog.
func (s Secret) LogValue() slog.Value { return slog.StringValue("[REDACTED]") }

// String скрывает значение в fmt (%v, %s, Println).
func (s Secret) String() string { return "[REDACTED]" }

// GoString скрывает значение в %#v.
func (s Secret) GoString() string { return `"[REDACTED]"` }

// MarshalText скрывает значение, когда Secret — поле структуры, которую
// сериализуют целиком: encoding/json, slog.Any(cfg) с JSONHandler, YAML.
func (s Secret) MarshalText() ([]byte, error) { return []byte("[REDACTED]"), nil }
