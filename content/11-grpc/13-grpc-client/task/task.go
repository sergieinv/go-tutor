package task

import (
	"context"
	"time"

	"google.golang.org/grpc"
)

// Config — настройки повторов клиента.
type Config struct {
	// MaxAttempts — всего попыток, включая первую; меньше 1 — одна попытка.
	MaxAttempts int
	// InitialBackoff и MaxBackoff — пауза перед повтором, если сервер
	// не прислал RetryInfo: InitialBackoff·2^(n-1) для n-го повтора,
	// не больше MaxBackoff (0 — без потолка).
	InitialBackoff time.Duration
	MaxBackoff     time.Duration
	// Sleep ждёт d или отмены ctx (тогда возвращает ошибку).
	Sleep func(ctx context.Context, d time.Duration) error
	// Now — текущее время для проверки дедлайна.
	Now func() time.Time
}

// UnaryRetry возвращает клиентский interceptor с повторами.
func UnaryRetry(cfg Config) grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// TODO: попытки, RetryInfo, backoff, дедлайн, x-attempt.
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
