package grpcx

import (
	"context"
	"errors"
	"log/slog"
	"runtime/debug"
	"strings"
	"time"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"

	"gotutor/labs/platform/logging"
)

// RequestIDKey — ключ metadata с id запроса. Строчными: в HTTP/2 имена
// заголовков всегда в нижнем регистре.
const RequestIDKey = "x-request-id"

// RequestIDUnary берёт id запроса из metadata x-request-id (если он похож
// на id) или создаёт новый, кладёт в контекст и возвращает клиенту в
// заголовке ответа.
func RequestIDUnary() grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		id := ""
		if md, ok := metadata.FromIncomingContext(ctx); ok {
			if v := md.Get(RequestIDKey); len(v) > 0 && logging.ValidRequestID(v[0]) {
				id = v[0]
			}
		}
		if id == "" {
			id = logging.NewRequestID()
		}
		_ = grpc.SetHeader(ctx, metadata.Pairs(RequestIDKey, id))
		return handler(logging.WithRequestID(ctx, id), req)
	}
}

// LoggingUnary пишет строку журнала на каждый вызов: метод, код, время.
// Текст ошибки — только в журнал; что увидит клиент, решает обработчик.
func LoggingUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		st := status.Convert(err)
		level := slog.LevelInfo
		switch {
		case strings.HasPrefix(info.FullMethod, "/grpc.health."):
			level = slog.LevelDebug // пробы раз в несколько секунд — шум
		case isServerFault(st.Code()):
			level = slog.LevelError
		}
		attrs := []any{
			"method", info.FullMethod,
			"code", st.Code().String(),
			"duration_ms", time.Since(start).Milliseconds(),
		}
		if err != nil {
			attrs = append(attrs, "err", err)
		}
		log.Log(ctx, level, "grpc call", attrs...)
		return resp, err
	}
}

// isServerFault — коды, которые означают проблему сервера, а не клиента.
func isServerFault(c codes.Code) bool {
	switch c {
	case codes.Internal, codes.Unknown, codes.DataLoss, codes.Unavailable, codes.DeadlineExceeded, codes.Unimplemented:
		return true
	}
	return false
}

// RecoveryUnary превращает панику обработчика в codes.Internal без
// подробностей. Стек — в журнал. Без этого паника в одном обработчике
// роняет весь процесс со всеми остальными запросами.
func RecoveryUnary(log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (resp any, err error) {
		defer func() {
			if v := recover(); v != nil {
				log.ErrorContext(ctx, "panic", "method", info.FullMethod, "panic", v, "stack", string(debug.Stack()))
				resp, err = nil, status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(ctx, req)
	}
}

// RecoveryStream — то же для потоковых вызовов (health Watch, reflection).
func RecoveryStream(log *slog.Logger) grpc.StreamServerInterceptor {
	return func(srv any, ss grpc.ServerStream, info *grpc.StreamServerInfo, handler grpc.StreamHandler) (err error) {
		defer func() {
			if v := recover(); v != nil {
				log.ErrorContext(ss.Context(), "panic", "method", info.FullMethod, "panic", v, "stack", string(debug.Stack()))
				err = status.Error(codes.Internal, "internal error")
			}
		}()
		return handler(srv, ss)
	}
}

// TimeoutUnary задаёт дедлайн обработки, если клиент его не прислал.
// Сервер не доверяет клиенту в вопросе «сколько можно работать»: вызов без
// дедлайна при зависшей базе держал бы горутину и соединение с базой
// бесконечно. Дедлайн клиента (grpc-timeout) уже в ctx — его не трогаем.
func TimeoutUnary(d time.Duration) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, d)
			defer cancel()
		}
		return handler(ctx, req)
	}
}

// ValidationUnary проверяет запрос по правилам protovalidate из контракта —
// один раз для всех методов, до обработчика. Нарушения уходят клиенту как
// INVALID_ARGUMENT с google.rpc.BadRequest: путь к полю и причина.
func ValidationUnary(v protovalidate.Validator, log *slog.Logger) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		msg, ok := req.(proto.Message)
		if !ok {
			return handler(ctx, req)
		}
		if err := v.Validate(msg); err != nil {
			var verr *protovalidate.ValidationError
			if errors.As(err, &verr) {
				return nil, violationsStatus(verr)
			}
			// Ошибка компиляции правил или выполнения CEL — дефект
			// контракта, а не плохой запрос.
			return nil, Internal(ctx, log, "validate "+info.FullMethod, err)
		}
		return handler(ctx, req)
	}
}

func violationsStatus(verr *protovalidate.ValidationError) error {
	fields := make([]*errdetails.BadRequest_FieldViolation, 0, len(verr.Violations))
	for _, v := range verr.Violations {
		fields = append(fields, &errdetails.BadRequest_FieldViolation{
			Field:       protovalidate.FieldPathString(v.Proto.GetField()),
			Description: v.Proto.GetMessage(),
			Reason:      v.Proto.GetRuleId(),
		})
	}
	return InvalidArgument("invalid request", fields...)
}

// InvalidArgument возвращает INVALID_ARGUMENT с google.rpc.BadRequest.
func InvalidArgument(msg string, fields ...*errdetails.BadRequest_FieldViolation) error {
	st := status.New(codes.InvalidArgument, msg)
	if len(fields) == 0 {
		return st.Err()
	}
	if withDetails, err := st.WithDetails(&errdetails.BadRequest{FieldViolations: fields}); err == nil {
		st = withDetails
	}
	return st.Err()
}

// Internal журналирует err и возвращает клиенту INTERNAL без подробностей:
// текст SQL-ошибки или адрес базы — не для клиента. log == nil — журнал из
// пакета slog по умолчанию.
func Internal(ctx context.Context, log *slog.Logger, what string, err error) error {
	if log == nil {
		log = slog.Default()
	}
	log.ErrorContext(ctx, what, "err", err)
	return status.Error(codes.Internal, "internal error")
}
