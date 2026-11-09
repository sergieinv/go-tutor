package task

import (
	"context"
	"crypto/subtle"
	"slices"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
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

type callerKey struct{}

// CallerFrom возвращает Caller, который UnaryAuth положил в контекст.
func CallerFrom(ctx context.Context) (Caller, bool) {
	c, ok := ctx.Value(callerKey{}).(Caller)
	return c, ok
}

var (
	errUnauthenticated = status.Error(codes.Unauthenticated, "unauthenticated")
	errDenied          = status.Error(codes.PermissionDenied, "permission denied")
)

// UnaryAuth возвращает auth-interceptor сервера по политике p.
func UnaryAuth(p Policy) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if slices.Contains(p.Public, info.FullMethod) {
			return handler(ctx, req)
		}
		service, ok := tlsIdentity(ctx)
		if !ok {
			return nil, errUnauthenticated
		}
		g, ok := lookupToken(ctx, p.Tokens)
		if !ok || g.Service != service {
			return nil, errUnauthenticated
		}
		allowed := false
		for _, r := range p.Methods[info.FullMethod] {
			if slices.Contains(g.Roles, r) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, errDenied
		}
		c := Caller{Service: service, Roles: slices.Clone(g.Roles)}
		return handler(context.WithValue(ctx, callerKey{}, c), req)
	}
}

// tlsIdentity — CN листового сертификата из проверенной цепочки.
// PeerCertificates без VerifiedChains — то, что клиент заявил о себе,
// а не то, что подтвердил CA.
func tlsIdentity(ctx context.Context) (string, bool) {
	pr, ok := peer.FromContext(ctx)
	if !ok {
		return "", false
	}
	ti, ok := pr.AuthInfo.(credentials.TLSInfo)
	if !ok || len(ti.State.VerifiedChains) == 0 || len(ti.State.VerifiedChains[0]) == 0 {
		return "", false
	}
	cn := ti.State.VerifiedChains[0][0].Subject.CommonName
	return cn, cn != ""
}

// lookupToken ищет токен из единственного заголовка authorization.
// Перебираем все токены и сравниваем за постоянное время.
func lookupToken(ctx context.Context, tokens map[string]Grant) (Grant, bool) {
	md, _ := metadata.FromIncomingContext(ctx)
	vals := md.Get("authorization")
	if len(vals) != 1 {
		return Grant{}, false
	}
	tok, ok := strings.CutPrefix(vals[0], "Bearer ")
	if !ok || tok == "" {
		return Grant{}, false
	}
	var found Grant
	match := false
	for t, g := range tokens {
		if subtle.ConstantTimeCompare([]byte(tok), []byte(t)) == 1 {
			found, match = g, true
		}
	}
	return found, match
}
