package task

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"strings"
	"testing"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// plainAuth — AuthInfo соединения без TLS.
type plainAuth struct{}

func (plainAuth) AuthType() string { return "insecure" }

func expectRejected(t *testing.T, r callResult, want codes.Code, what string) {
	t.Helper()
	if r.called {
		t.Fatalf("%s: обработчик вызван — атака прошла", what)
	}
	if got := status.Code(r.err); got != want {
		t.Fatalf("%s: код = %v, want %v (err = %v)", what, got, want, r.err)
	}
}

func TestExploitTokenWithoutTLS(t *testing.T) {
	// Верный токен, но соединение без TLS: личность не подтверждена.
	noPeer := withAuth(context.Background(), "Bearer "+billingToken)
	expectRejected(t, call(testPolicy(), noPeer, methodGet), codes.Unauthenticated, "без peer")

	plain := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: plainAuth{}})
	expectRejected(t, call(testPolicy(), withAuth(plain, "Bearer "+billingToken), methodGet), codes.Unauthenticated, "соединение без TLS")
}

func TestExploitUnverifiedCertificate(t *testing.T) {
	// Сервер с RequestClientCert/RequireAnyClientCert кладёт сертификат клиента
	// в PeerCertificates, не проверяя его. CN там — что угодно.
	rogue := newTestCA(t, "rogue-ca")
	leaf := rogue.issue(t, "billing", false).Leaf
	ti := credentials.TLSInfo{State: tls.ConnectionState{PeerCertificates: []*x509.Certificate{leaf}}}
	ctx := peer.NewContext(context.Background(), &peer.Peer{AuthInfo: ti})
	expectRejected(t, call(testPolicy(), withAuth(ctx, "Bearer "+billingToken), methodGet), codes.Unauthenticated, "непроверенный сертификат")
}

func TestExploitUnverifiedCertificateEndToEnd(t *testing.T) {
	// Тот же обход на настоящем TLS: сервер просит сертификат, но не
	// проверяет цепочку (RequireAnyClientCert), клиент приносит
	// самодельный сертификат с CN billing и токен billing.
	env := startTLS(t, guardedHealth(), tls.RequireAnyClientCert)
	rogue := newTestCA(t, "rogue-ca")
	cert := rogue.issue(t, "billing", false)
	client := healthpb.NewHealthClient(env.dial(t, &cert))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.Check(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+billingToken), &healthpb.HealthCheckRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("самодельный сертификат с CN billing: %v, want Unauthenticated", err)
	}
}

func TestExploitStolenToken(t *testing.T) {
	// Токен ops-admin утёк, но сертификат у атакующего — от billing.
	ctx := withAuth(mtlsCtx(t, "billing"), "Bearer "+adminToken)
	expectRejected(t, call(testPolicy(), ctx, methodCancel), codes.Unauthenticated, "токен чужого сервиса")
}

func TestExploitUnlistedMethod(t *testing.T) {
	// Метода нет в Methods — доступа нет ни у кого, даже у admin.
	ctx := withAuth(mtlsCtx(t, "ops-admin"), "Bearer "+adminToken)
	expectRejected(t, call(testPolicy(), ctx, "/orders.v1.OrderService/DeleteOrder"), codes.PermissionDenied, "метод не из политики")

	p := testPolicy()
	p.Methods["/orders.v1.OrderService/Purge"] = nil
	expectRejected(t, call(p, ctx, "/orders.v1.OrderService/Purge"), codes.PermissionDenied, "метод с пустым списком ролей")
}

func TestExploitDuplicateAuthorization(t *testing.T) {
	for _, vals := range [][]string{
		{"Bearer nope", "Bearer " + billingToken},
		{"Bearer " + billingToken, "Bearer nope"},
		{"Bearer " + billingToken, "Bearer " + billingToken},
	} {
		ctx := withAuth(mtlsCtx(t, "billing"), vals...)
		expectRejected(t, call(testPolicy(), ctx, methodGet), codes.Unauthenticated, "несколько authorization "+strings.Join(vals, " | "))
	}
}

func TestExploitTokenPrefix(t *testing.T) {
	for _, v := range []string{
		"Bearer " + billingToken[:6],
		"Bearer " + billingToken + "x",
		"Bearer ",
		"Bearer",
		"bearer " + billingToken,
		"Basic " + billingToken,
	} {
		ctx := withAuth(mtlsCtx(t, "billing"), v)
		expectRejected(t, call(testPolicy(), ctx, methodGet), codes.Unauthenticated, "authorization "+v)
	}
}

func TestExploitErrorDoesNotEchoToken(t *testing.T) {
	const guess = "secret-guess-123"
	r := call(testPolicy(), withAuth(mtlsCtx(t, "billing"), "Bearer "+guess), methodGet)
	expectRejected(t, r, codes.Unauthenticated, "неизвестный токен")
	if strings.Contains(status.Convert(r.err).Message(), guess) {
		t.Fatalf("сообщение ошибки содержит присланный токен: %q", status.Convert(r.err).Message())
	}
	r = call(testPolicy(), withAuth(mtlsCtx(t, "billing"), "Bearer "+adminToken), methodCancel)
	expectRejected(t, r, codes.Unauthenticated, "токен чужого сервиса")
	if strings.Contains(status.Convert(r.err).Message(), adminToken) {
		t.Fatalf("сообщение ошибки содержит токен: %q", status.Convert(r.err).Message())
	}
}

func TestEmptyCommonName(t *testing.T) {
	ctx := withAuth(mtlsCtx(t, ""), "Bearer "+billingToken)
	expectRejected(t, call(testPolicy(), ctx, methodGet), codes.Unauthenticated, "сертификат без CN")
}

func TestIdentityCheckedFirst(t *testing.T) {
	// Сначала личность, потом токен, потом права.
	noTLS := withAuth(context.Background(), "Bearer "+adminToken)
	expectRejected(t, call(testPolicy(), noTLS, "/orders.v1.OrderService/DeleteOrder"), codes.Unauthenticated, "без TLS, метод не из политики")
	noToken := mtlsCtx(t, "ops-admin")
	expectRejected(t, call(testPolicy(), noToken, "/orders.v1.OrderService/DeleteOrder"), codes.Unauthenticated, "без токена, метод не из политики")
}

func TestPublicMethodWithoutCaller(t *testing.T) {
	r := call(testPolicy(), withAuth(context.Background(), "Bearer garbage"), methodHealth)
	if r.err != nil || !r.called {
		t.Fatalf("публичный метод: err = %v, обработчик вызван = %v", r.err, r.called)
	}
	if r.hasCal {
		t.Fatalf("публичный метод: в контексте Caller %+v, want нет", r.caller)
	}
	if _, ok := CallerFrom(context.Background()); ok {
		t.Fatal("CallerFrom(context.Background()) = true, want false")
	}
}

func TestRequireAndVerifyRejectsRogueCA(t *testing.T) {
	// С правильной настройкой сервера самодельный сертификат не проходит
	// рукопожатие, и до interceptor вызов не доходит.
	env := startTLS(t, guardedHealth(), tls.RequireAndVerifyClientCert)
	rogue := newTestCA(t, "rogue-ca")
	cert := rogue.issue(t, "billing", false)
	client := healthpb.NewHealthClient(env.dial(t, &cert))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := client.Check(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+billingToken), &healthpb.HealthCheckRequest{})
	if err == nil {
		t.Fatal("сертификат чужого CA принят")
	}
}
