package task

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"math/big"
	"net"
	"slices"
	"testing"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

const (
	methodGet    = "/orders.v1.OrderService/GetOrder"
	methodCancel = "/orders.v1.OrderService/CancelOrder"
	methodHealth = "/grpc.health.v1.Health/Check"

	billingToken = "tok-billing-7f3a9c"
	adminToken   = "tok-admin-41d2e8"
)

func testPolicy() Policy {
	return Policy{
		Tokens: map[string]Grant{
			billingToken: {Service: "billing", Roles: []string{"reader"}},
			adminToken:   {Service: "ops-admin", Roles: []string{"reader", "admin"}},
		},
		Methods: map[string][]string{
			methodGet:    {"reader"},
			methodCancel: {"admin"},
		},
		Public: []string{methodHealth},
	}
}

// --- сертификаты в памяти ---

type testCA struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newTestCA(t *testing.T, name string) *testCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: name},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &testCA{cert: cert, key: key, pool: pool}
}

func (c *testCA) issue(t *testing.T, cn string, server bool) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"orders.internal"}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key, Leaf: leaf}
}

// --- прямой вызов interceptor ---

// mtlsCtx — контекст, как его видит сервер после mTLS: цепочка клиента
// с CN cn проверена по CA.
func mtlsCtx(t *testing.T, cn string) context.Context {
	t.Helper()
	ca := newTestCA(t, "test-ca")
	leaf := ca.issue(t, cn, false).Leaf
	ti := credentials.TLSInfo{State: tls.ConnectionState{
		PeerCertificates: []*x509.Certificate{leaf},
		VerifiedChains:   [][]*x509.Certificate{{leaf, ca.cert}},
	}}
	return peer.NewContext(context.Background(), &peer.Peer{AuthInfo: ti})
}

func withAuth(ctx context.Context, vals ...string) context.Context {
	md := metadata.MD{}
	for _, v := range vals {
		md.Append("authorization", v)
	}
	return metadata.NewIncomingContext(ctx, md)
}

type callResult struct {
	err    error
	called bool
	caller Caller
	hasCal bool
}

func call(p Policy, ctx context.Context, method string) callResult {
	var r callResult
	h := func(ctx context.Context, req any) (any, error) {
		r.called = true
		r.caller, r.hasCal = CallerFrom(ctx)
		return "ok", nil
	}
	_, r.err = UnaryAuth(p)(ctx, "req", &grpc.UnaryServerInfo{FullMethod: method}, h)
	return r
}

func TestUnaryAuth(t *testing.T) {
	tests := []struct {
		name   string
		cn     string
		auth   []string
		method string
		want   codes.Code
	}{
		{"reader читает", "billing", []string{"Bearer " + billingToken}, methodGet, codes.OK},
		{"admin отменяет", "ops-admin", []string{"Bearer " + adminToken}, methodCancel, codes.OK},
		{"reader не отменяет", "billing", []string{"Bearer " + billingToken}, methodCancel, codes.PermissionDenied},
		{"нет токена", "billing", nil, methodGet, codes.Unauthenticated},
		{"неизвестный токен", "billing", []string{"Bearer nope"}, methodGet, codes.Unauthenticated},
		{"токен без схемы Bearer", "billing", []string{billingToken}, methodGet, codes.Unauthenticated},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := call(testPolicy(), withAuth(mtlsCtx(t, tt.cn), tt.auth...), tt.method)
			if got := status.Code(r.err); got != tt.want {
				t.Fatalf("код = %v, want %v (err = %v)", got, tt.want, r.err)
			}
			if r.called != (tt.want == codes.OK) {
				t.Fatalf("handler вызван = %v, want %v", r.called, tt.want == codes.OK)
			}
		})
	}
}

func TestCallerInContext(t *testing.T) {
	r := call(testPolicy(), withAuth(mtlsCtx(t, "ops-admin"), "Bearer "+adminToken), methodGet)
	if r.err != nil {
		t.Fatalf("err = %v", r.err)
	}
	if !r.hasCal || r.caller.Service != "ops-admin" || !slices.Equal(r.caller.Roles, []string{"reader", "admin"}) {
		t.Fatalf("CallerFrom = %+v, %v, want {ops-admin [reader admin]}, true", r.caller, r.hasCal)
	}
}

func TestPublicMethod(t *testing.T) {
	// Публичный метод доступен и без TLS, и без токена.
	r := call(testPolicy(), context.Background(), methodHealth)
	if r.err != nil || !r.called {
		t.Fatalf("публичный метод: err = %v, handler вызван = %v", r.err, r.called)
	}
}

// --- настоящий mTLS через bufconn ---

type tlsEnv struct {
	ca     *testCA
	server *grpc.Server
	lis    *bufconn.Listener
	seen   chan Caller
}

// startTLS поднимает gRPC-сервер с mTLS и health-сервисом на bufconn.
// Второй interceptor после UnaryAuth сообщает, кого увидел обработчик.
func startTLS(t *testing.T, p Policy, clientAuth tls.ClientAuthType) *tlsEnv {
	t.Helper()
	env := &tlsEnv{ca: newTestCA(t, "orders-ca"), lis: bufconn.Listen(1 << 20), seen: make(chan Caller, 16)}
	srvCert := env.ca.issue(t, "orders", true)
	cfg := &tls.Config{
		Certificates: []tls.Certificate{srvCert},
		ClientAuth:   clientAuth,
		MinVersion:   tls.VersionTLS13,
	}
	// ClientCAs нужен только режимам с проверкой цепочки. Сервер с ним
	// сообщает клиенту список CA, и клиент не предъявит сертификат другого CA.
	if clientAuth == tls.VerifyClientCertIfGiven || clientAuth == tls.RequireAndVerifyClientCert {
		cfg.ClientCAs = env.ca.pool
	}
	creds := credentials.NewTLS(cfg)
	spy := func(ctx context.Context, req any, info *grpc.UnaryServerInfo, h grpc.UnaryHandler) (any, error) {
		if c, ok := CallerFrom(ctx); ok {
			env.seen <- c
		}
		return h(ctx, req)
	}
	env.server = grpc.NewServer(grpc.Creds(creds), grpc.ChainUnaryInterceptor(UnaryAuth(p), spy))
	hs := health.NewServer()
	healthpb.RegisterHealthServer(env.server, hs)
	go env.server.Serve(env.lis)
	t.Cleanup(env.server.Stop)
	return env
}

func (env *tlsEnv) dial(t *testing.T, cert *tls.Certificate) *grpc.ClientConn {
	t.Helper()
	cfg := &tls.Config{RootCAs: env.ca.pool, ServerName: "orders.internal", MinVersion: tls.VersionTLS13}
	if cert != nil {
		cfg.Certificates = []tls.Certificate{*cert}
	}
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return env.lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(credentials.NewTLS(cfg)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

// Метод health/Check в этой политике защищён: роль reader.
func guardedHealth() Policy {
	p := testPolicy()
	p.Public = nil
	p.Methods[methodHealth] = []string{"reader"}
	return p
}

func TestEndToEndMTLS(t *testing.T) {
	env := startTLS(t, guardedHealth(), tls.RequireAndVerifyClientCert)
	cert := env.ca.issue(t, "billing", false)
	client := healthpb.NewHealthClient(env.dial(t, &cert))
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	_, err := client.Check(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+billingToken), &healthpb.HealthCheckRequest{})
	if err != nil {
		t.Fatalf("Check с сертификатом billing и его токеном: %v", err)
	}
	select {
	case c := <-env.seen:
		if c.Service != "billing" {
			t.Fatalf("Caller.Service = %q, want billing", c.Service)
		}
	default:
		t.Fatal("обработчик не получил Caller в контексте")
	}

	_, err = client.Check(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+adminToken), &healthpb.HealthCheckRequest{})
	if status.Code(err) != codes.Unauthenticated {
		t.Fatalf("Check с сертификатом billing и токеном ops-admin: %v, want Unauthenticated", err)
	}
}
