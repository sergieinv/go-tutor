// Package grpcx — gRPC-сервер и клиент сервиса с общими настройками:
// лимиты, keepalive, трассировка otelgrpc и цепочка interceptor'ов
// (id запроса, журнал, метрики, recovery, дедлайн по умолчанию,
// валидация protovalidate).
//
// Interceptor — аналог Interceptor из Grpc.AspNetCore. Цепочка задаётся
// явно в одном месте (NewServer), первый в списке — внешний.
package grpcx

import (
	"log/slog"
	"time"

	"buf.build/go/protovalidate"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc"
	"go.opentelemetry.io/contrib/instrumentation/google.golang.org/grpc/otelgrpc/filters"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
)

// Лимиты по умолчанию. Сообщения сервисов — килобайты, поэтому лимит на
// входящее сообщение много меньше стандартных 4 МиБ: каждое входящее
// сообщение целиком лежит в памяти, и большой лимит — простой способ
// съесть память сервера.
const (
	DefaultMaxRecvMsgSize = 256 << 10
	DefaultTimeout        = 10 * time.Second
)

// ServerConfig — настройки сервера.
type ServerConfig struct {
	Logger *slog.Logger
	// Metrics — RED-метрики; nil — без метрик.
	Metrics *ServerMetrics
	// Validator проверяет каждый запрос по правилам protovalidate из
	// контракта; nil — без валидации.
	Validator protovalidate.Validator
	// DefaultTimeout — дедлайн обработки, если клиент его не задал.
	// 0 — DefaultTimeout.
	DefaultTimeout time.Duration
	// MaxRecvMsgSize — лимит входящего сообщения; 0 — DefaultMaxRecvMsgSize.
	MaxRecvMsgSize int
	// Creds — TLS/mTLS; nil — без шифрования (только внутри docker-сети
	// labs, см. labs/README.md).
	Creds credentials.TransportCredentials
}

// NewServer создаёт gRPC-сервер. Сервисы регистрирует вызывающий.
func NewServer(cfg ServerConfig, extra ...grpc.ServerOption) *grpc.Server {
	if cfg.DefaultTimeout == 0 {
		cfg.DefaultTimeout = DefaultTimeout
	}
	if cfg.MaxRecvMsgSize == 0 {
		cfg.MaxRecvMsgSize = DefaultMaxRecvMsgSize
	}
	creds := cfg.Creds
	if creds == nil {
		creds = insecure.NewCredentials()
	}

	// Порядок: первый — внешний. Id запроса нужен всем, журнал и метрики
	// видят итоговый код (в том числе Internal после паники), recovery
	// ловит панику любого внутреннего звена, валидация — последней, перед
	// обработчиком.
	unary := []grpc.UnaryServerInterceptor{
		RequestIDUnary(),
		LoggingUnary(cfg.Logger),
	}
	if cfg.Metrics != nil {
		unary = append(unary, cfg.Metrics.UnaryInterceptor())
	}
	unary = append(unary, RecoveryUnary(cfg.Logger), TimeoutUnary(cfg.DefaultTimeout))
	if cfg.Validator != nil {
		unary = append(unary, ValidationUnary(cfg.Validator, cfg.Logger))
	}

	opts := []grpc.ServerOption{
		grpc.Creds(creds),
		// Трассировка — stats handler, а не interceptor: спан открывается
		// раньше всех interceptor'ов, и журнал видит trace_id. Health-пробы
		// не трассируем — это шум.
		grpc.StatsHandler(otelgrpc.NewServerHandler(
			otelgrpc.WithFilter(filters.Not(filters.HealthCheck())),
		)),
		grpc.ChainUnaryInterceptor(unary...),
		grpc.ChainStreamInterceptor(RecoveryStream(cfg.Logger)),
		grpc.MaxRecvMsgSize(cfg.MaxRecvMsgSize),
		grpc.MaxSendMsgSize(4 << 20),
		// Не больше стольких одновременных вызовов на одно соединение.
		grpc.MaxConcurrentStreams(256),
		grpc.KeepaliveParams(keepalive.ServerParameters{
			// Простаивающее соединение закрывается — не копим мёртвые.
			MaxConnectionIdle: 5 * time.Minute,
			// Соединение периодически пересоздаётся: клиенты за
			// балансировщиком L4 перераспределяются по новым репликам.
			MaxConnectionAge:      30 * time.Minute,
			MaxConnectionAgeGrace: 30 * time.Second,
		}),
		// Клиент, который пингует чаще раза в 10 секунд, получает GOAWAY:
		// защита от пинг-флуда.
		grpc.KeepaliveEnforcementPolicy(keepalive.EnforcementPolicy{
			MinTime:             10 * time.Second,
			PermitWithoutStream: true,
		}),
	}
	return grpc.NewServer(append(opts, extra...)...)
}
