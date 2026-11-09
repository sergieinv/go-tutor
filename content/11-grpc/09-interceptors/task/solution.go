package task

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
)

// Logger получает готовые строки лога.
type Logger func(line string)

// Config — зависимости цепочки.
type Config struct {
	Log    Logger
	NewID  func() string     // request id, если клиент его не прислал
	Tokens map[string]string // пользователь → токен
}

type ridKey struct{}
type userKey struct{}

// RequestIDFrom возвращает request id запроса ("" — его нет).
func RequestIDFrom(ctx context.Context) string {
	s, _ := ctx.Value(ridKey{}).(string)
	return s
}

// UserFrom возвращает пользователя, прошедшего auth ("" — нет).
func UserFrom(ctx context.Context) string {
	s, _ := ctx.Value(userKey{}).(string)
	return s
}

// Chain возвращает серверную цепочку от внешнего звена к внутреннему:
// request id → logging → recovery → auth.
func Chain(cfg Config) []grpc.UnaryServerInterceptor {
	return []grpc.UnaryServerInterceptor{
		requestID(cfg.NewID),
		logging(cfg.Log),
		recovery(cfg.Log),
		auth(cfg.Tokens),
	}
}

func requestID(newID func() string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		rid := ""
		if v := metadata.ValueFromIncomingContext(ctx, "x-request-id"); len(v) > 0 {
			rid = v[0]
		}
		if rid == "" {
			rid = newID()
		}
		// Ошибка бывает только вне настоящего вызова gRPC — тогда некуда отправлять.
		_ = grpc.SetHeader(ctx, metadata.Pairs("x-request-id", rid))
		return handler(context.WithValue(ctx, ridKey{}, rid), req)
	}
}

func logging(log Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		resp, err := handler(ctx, req)
		log(fmt.Sprintf("%s %s rid=%s", info.FullMethod, status.Code(err), RequestIDFrom(ctx)))
		return resp, err
	}
}

func recovery(log Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if p := recover(); p != nil {
				log(fmt.Sprintf("panic %s rid=%s: %v", info.FullMethod, RequestIDFrom(ctx), p))
				resp, err = nil, status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

func auth(tokens map[string]string) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if strings.HasPrefix(info.FullMethod, "/grpc.health.v1.Health/") {
			return handler(ctx, req)
		}
		denied := status.Error(codes.Unauthenticated, "unauthenticated")
		v := metadata.ValueFromIncomingContext(ctx, "authorization")
		if len(v) != 1 {
			return nil, denied
		}
		token, ok := strings.CutPrefix(v[0], "Bearer ")
		if !ok || token == "" {
			return nil, denied
		}
		user := ""
		for u, t := range tokens {
			// Сравниваем со всеми, без раннего выхода по совпадению имени.
			if subtle.ConstantTimeCompare([]byte(token), []byte(t)) == 1 {
				user = u
			}
		}
		if user == "" {
			return nil, denied
		}
		return handler(context.WithValue(ctx, userKey{}, user), req)
	}
}
