package task

import (
	"context"
	"strconv"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
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
	attempts := max(cfg.MaxAttempts, 1)
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		// Исходная metadata вызывающего — основа для каждой попытки:
		// x-attempt заменяется, а не копится.
		base, _ := metadata.FromOutgoingContext(ctx)
		for attempt := 1; ; attempt++ {
			md := base.Copy()
			md.Set("x-attempt", strconv.Itoa(attempt))
			err := invoker(metadata.NewOutgoingContext(ctx, md), method, req, reply, cc, opts...)
			if err == nil {
				return nil
			}
			if attempt >= attempts {
				return err
			}
			delay, ok := retryDelay(status.Convert(err), attempt, cfg)
			if !ok {
				return err
			}
			// Не ждём впустую: после паузы дедлайн уже истечёт.
			if dl, has := ctx.Deadline(); has && delay >= dl.Sub(cfg.Now()) {
				return err
			}
			if serr := cfg.Sleep(ctx, delay); serr != nil {
				return status.FromContextError(serr).Err()
			}
		}
	}
}

// retryDelay решает, повторять ли, и возвращает паузу перед повтором
// номер retry (1 — первый повтор).
func retryDelay(st *status.Status, retry int, cfg Config) (time.Duration, bool) {
	var info *errdetails.RetryInfo
	for _, d := range st.Details() {
		if ri, ok := d.(*errdetails.RetryInfo); ok && ri.GetRetryDelay() != nil {
			info = ri
			break
		}
	}
	switch st.Code() {
	case codes.Unavailable, codes.Aborted:
	case codes.ResourceExhausted:
		// Без RetryInfo это может быть «сообщение больше лимита» —
		// повтор ничего не изменит.
		if info == nil {
			return 0, false
		}
	default:
		return 0, false
	}
	if info != nil {
		return max(info.GetRetryDelay().AsDuration(), 0), true
	}
	d := cfg.InitialBackoff
	for i := 1; i < retry; i++ {
		d *= 2
		if cfg.MaxBackoff > 0 && d >= cfg.MaxBackoff {
			break
		}
	}
	if cfg.MaxBackoff > 0 {
		d = min(d, cfg.MaxBackoff)
	}
	return d, true
}
