package task

import (
	"context"

	"google.golang.org/grpc"
)

// Logger получает готовые строки лога.
type Logger func(line string)

// Config — зависимости цепочки.
type Config struct {
	Log    Logger
	NewID  func() string     // request id, если клиент его не прислал
	Tokens map[string]string // пользователь → токен
}

// RequestIDFrom возвращает request id запроса ("" — его нет).
func RequestIDFrom(ctx context.Context) string {
	// TODO
	return ""
}

// UserFrom возвращает пользователя, прошедшего auth ("" — нет).
func UserFrom(ctx context.Context) string {
	// TODO
	return ""
}

// Chain возвращает серверную цепочку от внешнего звена к внутреннему:
// request id → logging → recovery → auth.
func Chain(cfg Config) []grpc.UnaryServerInterceptor {
	// TODO
	return nil
}
