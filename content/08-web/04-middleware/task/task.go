package task

import (
	"context"
	"net/http"
)

// Middleware оборачивает хендлер.
type Middleware func(http.Handler) http.Handler

// Chain применяет middleware: первая в списке — внешняя.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	// TODO: реализуйте функцию
	return h
}

// Entry — запись журнала запроса.
type Entry struct {
	Method string // r.Method
	Path   string // r.URL.Path
	Status int    // статус ответа
	Bytes  int    // сколько байт тела записано
}

// Logging вызывает record после обработки каждого запроса.
func Logging(record func(Entry)) Middleware {
	// TODO: реализуйте функцию
	return func(next http.Handler) http.Handler { return next }
}

// RequestIDHeader — заголовок с id запроса.
const RequestIDHeader = "X-Request-ID"

// RequestID назначает запросу id и кладёт его в контекст.
func RequestID(gen func() string) Middleware {
	// TODO: реализуйте функцию
	return func(next http.Handler) http.Handler { return next }
}

// RequestIDFrom возвращает id запроса из контекста или "".
func RequestIDFrom(ctx context.Context) string {
	// TODO: реализуйте функцию
	return ""
}
