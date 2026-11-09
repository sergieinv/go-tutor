// Сервис заказов принимает только mTLS: сервер и клиенты
// предъявляют сертификаты одного внутреннего CA. Поверх
// канала клиент шлёт токен в metadata, а auth-interceptor
// сверяет три вещи: проверенный сертификат клиента, токен
// и права роли на метод. Сертификаты создаются в памяти —
// никаких файлов и ключей в репозитории.
package main

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/subtle"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"fmt"
	"log"
	"math/big"
	"net"
	"strings"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/peer"
	"google.golang.org/grpc/status"
)

// Учебный CA: ECDSA P-256 и короткий срок жизни. В проде
// сертификаты выдаёт cert-manager, Vault или service mesh,
// а сервис читает их из файлов, смонтированных из Secret.
type ca struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
	pool *x509.CertPool
}

func newCA(name string) (*ca, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
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
		return nil, err
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, err
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &ca{cert: cert, key: key, pool: pool}, nil
}

// `issue` выпускает листовой сертификат. Серверу нужен
// `ExtKeyUsageServerAuth` и имя в SAN (клиент сверит его
// с адресом), клиенту — `ExtKeyUsageClientAuth`.
func (c *ca) issue(cn string, server bool) (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 64))
	if err != nil {
		return tls.Certificate{}, err
	}
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth},
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.IPv4(127, 0, 0, 1)}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, c.cert, &key.PublicKey, c.key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// Токен выдан конкретному сервису и несёт его роли.
// Права на методы — явный список: метода нет в списке —
// доступа нет (deny by default).
type grant struct {
	service string
	roles   []string
}

var methodRoles = map[string][]string{
	OrderService_GetOrder_FullMethodName:    {"reader", "admin"},
	OrderService_CancelOrder_FullMethodName: {"admin"},
}

type callerKey struct{}

// Auth-interceptor. Сначала личность из TLS: `peer` даёт
// `credentials.TLSInfo`, а `VerifiedChains` не пуст, только
// если цепочка клиента проверена по нашему CA. Потом токен:
// сравнение через `subtle.ConstantTimeCompare`, без раннего
// выхода по первому несовпавшему байту. Ответы — без
// подробностей: какой именно шаг не прошёл, знает только лог.
func authInterceptor(tokens map[string]grant) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		p, ok := peer.FromContext(ctx)
		if !ok {
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		ti, ok := p.AuthInfo.(credentials.TLSInfo)
		if !ok || len(ti.State.VerifiedChains) == 0 {
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		service := ti.State.VerifiedChains[0][0].Subject.CommonName

		var g *grant
		md, _ := metadata.FromIncomingContext(ctx)
		if v := md.Get("authorization"); len(v) == 1 {
			got, _ := strings.CutPrefix(v[0], "Bearer ")
			for tok, gr := range tokens {
				if subtle.ConstantTimeCompare([]byte(got), []byte(tok)) == 1 {
					g = &gr
				}
			}
		}
		// Токен чужого сервиса — тоже «не аутентифицирован»:
		// украденный токен без ключа его сертификата бесполезен.
		if g == nil || g.service != service {
			return nil, status.Error(codes.Unauthenticated, "unauthenticated")
		}
		for _, r := range methodRoles[info.FullMethod] {
			for _, have := range g.roles {
				if r == have {
					return handler(context.WithValue(ctx, callerKey{}, service), req)
				}
			}
		}
		return nil, status.Error(codes.PermissionDenied, "permission denied")
	}
}

type ordersServer struct {
	UnimplementedOrderServiceServer
}

func (ordersServer) GetOrder(ctx context.Context, req *GetOrderRequest) (*GetOrderResponse, error) {
	caller, _ := ctx.Value(callerKey{}).(string)
	return &GetOrderResponse{Id: req.GetId(), Status: "created", Caller: caller}, nil
}

func (ordersServer) CancelOrder(ctx context.Context, req *CancelOrderRequest) (*CancelOrderResponse, error) {
	return &CancelOrderResponse{Id: req.GetId(), Status: "cancelled"}, nil
}

// Токен уходит в metadata каждого вызова через
// `PerRPCCredentials`. `RequireTransportSecurity() == true`
// запрещает отправить его по каналу без TLS.
type bearer string

func (b bearer) GetRequestMetadata(context.Context, ...string) (map[string]string, error) {
	return map[string]string{"authorization": "Bearer " + string(b)}, nil
}

func (bearer) RequireTransportSecurity() bool { return true }

func main() {
	internal, err := newCA("orders-internal-ca")
	if err != nil {
		log.Fatal(err)
	}
	serverCert, _ := internal.issue("orders", true)
	billingCert, _ := internal.issue("billing", false)
	adminCert, _ := internal.issue("ops-admin", false)

	// Сервер: TLS 1.3 и обязательный клиентский сертификат,
	// проверенный по `ClientCAs`. Лимит входящего сообщения —
	// явно (по умолчанию 4 МиБ).
	srvTLS := &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientCAs:    internal.pool,
		ClientAuth:   tls.RequireAndVerifyClientCert,
		MinVersion:   tls.VersionTLS13,
	}
	// Токены в проде читают из окружения или файла Secret.
	// Здесь — случайные, и в вывод они не попадают.
	billingTok, adminTok := rand.Text(), rand.Text()
	srv := grpc.NewServer(
		grpc.Creds(credentials.NewTLS(srvTLS)),
		grpc.MaxRecvMsgSize(1<<20),
		grpc.UnaryInterceptor(authInterceptor(map[string]grant{
			billingTok: {service: "billing", roles: []string{"reader"}},
			adminTok:   {service: "ops-admin", roles: []string{"admin"}},
		})),
	)
	RegisterOrderServiceServer(srv, ordersServer{})
	lis, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(lis)
	defer srv.Stop()
	addr := lis.Addr().String()

	// Клиент доверяет только нашему CA (`RootCAs`) и
	// предъявляет свой сертификат. Без `RootCAs` проверка шла
	// бы по системным корням, и внутренний CA бы не прошёл.
	var conns []*grpc.ClientConn
	defer func() {
		for _, c := range conns {
			c.Close()
		}
	}()
	dial := func(cert *tls.Certificate, roots *x509.CertPool, tok string) OrderServiceClient {
		cfg := &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS13}
		if cert != nil {
			cfg.Certificates = []tls.Certificate{*cert}
		}
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(credentials.NewTLS(cfg)),
			grpc.WithPerRPCCredentials(bearer(tok)))
		if err != nil {
			log.Fatal(err)
		}
		conns = append(conns, conn)
		return NewOrderServiceClient(conn)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	show := func(who string, resp any, err error) {
		if err != nil {
			fmt.Printf("%-34s → %s\n", who, status.Code(err))
			return
		}
		fmt.Printf("%-34s → OK %v\n", who, resp)
	}

	// Полный набор: сертификат, токен, роль. Ответ сервера
	// показывает, что личность взята из сертификата.
	billing := dial(&billingCert, internal.pool, billingTok)
	r1, err := billing.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
	show("billing: GetOrder", r1.GetCaller(), err)
	_, err = billing.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"})
	show("billing: CancelOrder", nil, err)

	admin := dial(&adminCert, internal.pool, adminTok)
	r2, err := admin.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"})
	show("ops-admin: CancelOrder", r2.GetStatus(), err)

	// Токен админа утёк, но сертификат у злоумышленника —
	// от billing: токен привязан к сервису и не сработает.
	stolen := dial(&billingCert, internal.pool, adminTok)
	_, err = stolen.CancelOrder(ctx, &CancelOrderRequest{Id: "ord-1"})
	show("billing + токен ops-admin", nil, err)

	// Без клиентского сертификата сервер рвёт рукопожатие:
	// до interceptor дело не доходит, код — `Unavailable`.
	anon := dial(nil, internal.pool, billingTok)
	_, err = anon.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
	show("без клиентского сертификата", nil, err)

	// Сертификат чужого CA сервер тоже не примет.
	rogue, _ := newCA("rogue-ca")
	rogueCert, _ := rogue.issue("billing", false)
	fake := dial(&rogueCert, internal.pool, billingTok)
	_, err = fake.GetOrder(ctx, &GetOrderRequest{Id: "ord-1"})
	show("сертификат чужого CA", nil, err)

	// Токен с `RequireTransportSecurity` по каналу без TLS
	// клиент не отправит: `NewClient` вернёт ошибку сразу.
	_, err = grpc.NewClient(addr,
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithPerRPCCredentials(bearer(billingTok)))
	fmt.Println("insecure + токен:", err)
}
