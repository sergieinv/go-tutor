package grpcx

import (
	"context"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"

	"gotutor/labs/platform/logging"
)

// ClientConfig — настройки клиента.
type ClientConfig struct {
	// Metrics — RED-метрики исходящих вызовов; nil — без метрик.
	Metrics *ClientMetrics
	// Creds — TLS/mTLS; nil — без шифрования.
	Creds credentials.TransportCredentials
}

// NewClient создаёт соединение с сервисом. Одно соединение на сервис на
// весь процесс: grpc.ClientConn — пул HTTP/2-соединений с переподключением,
// а не «соединение на запрос». Подключается лениво — при первом вызове.
//
// target — адрес в синтаксисе gRPC: "dns:///warehouse:9090" резолвит все
// адреса имени и (с round_robin ниже) распределяет вызовы между репликами.
func NewClient(target string, cfg ClientConfig, extra ...grpc.DialOption) (*grpc.ClientConn, error) {
	creds := cfg.Creds
	if creds == nil {
		creds = insecure.NewCredentials()
	}
	unary := []grpc.UnaryClientInterceptor{RequestIDClient()}
	if cfg.Metrics != nil {
		unary = append(unary, cfg.Metrics.UnaryInterceptor())
	}
	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(creds),
		// Спан на каждую попытку вызова; traceparent уходит в metadata.
		grpc.WithStatsHandler(otelgrpc.NewClientHandler()),
		grpc.WithChainUnaryInterceptor(unary...),
		// pick_first (по умолчанию) отправил бы всё в первую реплику.
		grpc.WithDefaultServiceConfig(`{"loadBalancingConfig": [{"round_robin": {}}]}`),
		grpc.WithDefaultCallOptions(grpc.MaxCallRecvMsgSize(4 << 20)),
		// Пинг соединения раз в 30 секунд замечает «тихо умершее» соединение
		// (NAT, перезапуск узла) раньше, чем на нём зависнет вызов. Не чаще
		// EnforcementPolicy.MinTime сервера, иначе сервер разорвёт соединение.
		grpc.WithKeepaliveParams(keepalive.ClientParameters{
			Time:    30 * time.Second,
			Timeout: 10 * time.Second,
		}),
	}
	return grpc.NewClient(target, append(opts, extra...)...)
}

// RequestIDClient передаёт id запроса из контекста соседу в metadata
// x-request-id: одна и та же операция находится в журналах всех сервисов.
func RequestIDClient() grpc.UnaryClientInterceptor {
	return func(ctx context.Context, method string, req, reply any, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
		md, _ := metadata.FromOutgoingContext(ctx)
		// grpc-gateway уже мог положить id в metadata — не дублируем.
		if id := logging.RequestID(ctx); id != "" && len(md.Get(RequestIDKey)) == 0 {
			ctx = metadata.AppendToOutgoingContext(ctx, RequestIDKey, id)
		}
		return invoker(ctx, method, req, reply, cc, opts...)
	}
}
