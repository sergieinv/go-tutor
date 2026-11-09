package task

import (
	"context"

	"google.golang.org/grpc"
)

// Grant — кому выдан токен и какие роли он даёт.
type Grant struct {
	Service string
	Roles   []string
}

// Policy — настройки аутентификации и авторизации сервера.
type Policy struct {
	// Tokens: токен → кому он выдан.
	Tokens map[string]Grant
	// Methods: полное имя метода ("/pkg.Service/Method") → роли,
	// любая из которых даёт доступ.
	Methods map[string][]string
	// Public — методы без проверок (например, health).
	Public []string
}

// Caller — кто вызвал метод: сервис из сертификата и роли токена.
type Caller struct {
	Service string
	Roles   []string
}

// CallerFrom возвращает Caller, который UnaryAuth положил в контекст.
func CallerFrom(ctx context.Context) (Caller, bool) {
	// TODO
	return Caller{}, false
}

// UnaryAuth возвращает auth-interceptor сервера по политике p.
func UnaryAuth(p Policy) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// TODO: личность из mTLS, токен из metadata, права на метод.
		return handler(ctx, req)
	}
}
