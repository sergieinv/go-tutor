package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/grpc-ecosystem/grpc-gateway/v2/runtime"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"google.golang.org/genproto/googleapis/rpc/code"
	// Типы google.rpc.BadRequest, ErrorInfo… должны быть в реестре protobuf,
	// иначе protojson не сможет развернуть их из Any в details.
	_ "google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protojson"

	"gotutor/labs/platform/logging"
)

// NewGatewayMux возвращает маршрутизатор grpc-gateway с общими для сервисов
// настройками:
//
//   - JSON по правилам protojson с именами полей из .proto (customer_id, а
//     не customerId) и нулевыми значениями в ответе; неизвестные поля в
//     запросе — ошибка (опечатка не превратится молча в нулевое значение);
//   - id запроса из контекста уходит в gRPC как metadata x-request-id;
//   - ошибки — в едином формате ErrorBody (WriteError) с request_id;
//   - metadata и трейлеры ответа gRPC не превращаются в HTTP-заголовки
//     Grpc-Metadata-*: это внутренности сервиса, клиенту REST они не нужны
//     (X-Request-ID ставит middleware RequestID);
//   - шаблон маршрута — в метрики/журнал (SetRoute) и в имя спана.
func NewGatewayMux(log *slog.Logger, opts ...runtime.ServeMuxOption) *runtime.ServeMux {
	marshaler := &runtime.HTTPBodyMarshaler{Marshaler: &runtime.JSONPb{
		MarshalOptions:   protojson.MarshalOptions{UseProtoNames: true, EmitUnpopulated: true},
		UnmarshalOptions: protojson.UnmarshalOptions{DiscardUnknown: false},
	}}
	base := []runtime.ServeMuxOption{
		runtime.WithMarshalerOption(runtime.MIMEWildcard, marshaler),
		runtime.WithMetadata(func(ctx context.Context, _ *http.Request) metadata.MD {
			if id := logging.RequestID(ctx); id != "" {
				return metadata.Pairs("x-request-id", id)
			}
			return nil
		}),
		runtime.WithErrorHandler(gatewayErrorHandler(log)),
		runtime.WithOutgoingHeaderMatcher(dropMetadata),
		runtime.WithOutgoingTrailerMatcher(dropMetadata),
		runtime.WithMiddlewares(gatewayRoute),
	}
	return runtime.NewServeMux(append(base, opts...)...)
}

func dropMetadata(string) (string, bool) { return "", false }

// gatewayRoute сообщает шаблон маршрута (/v1/orders/{order_id=*}) метрикам,
// журналу и спану otelhttp, который до этого знал только метод.
func gatewayRoute(next runtime.HandlerFunc) runtime.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request, params map[string]string) {
		if p, ok := runtime.HTTPPattern(r.Context()); ok {
			route := p.String()
			SetRoute(r.Context(), route)
			span := trace.SpanFromContext(r.Context())
			span.SetName(r.Method + " " + route)
			span.SetAttributes(semconv.HTTPRoute(route))
		}
		next(w, r, params)
	}
}

// gatewayErrorHandler переводит ошибку gRPC в HTTP-ответ: код — по
// стандартной таблице grpc-gateway (NOT_FOUND → 404, INVALID_ARGUMENT → 400,
// FAILED_PRECONDITION → 400, UNAVAILABLE → 503…), тело — ErrorBody.
//
// Сообщения UNAVAILABLE, INTERNAL, UNKNOWN и DATA_LOSS заменяются общими:
// их может сформировать не наш сервер, а сам gateway или gRPC-клиент
// («connection refused: dial tcp 10.0.0.5:9090»), и тогда в ответ уехали бы
// внутренние адреса. Оригинал — в журнал.
func gatewayErrorHandler(log *slog.Logger) runtime.ErrorHandlerFunc {
	return func(ctx context.Context, _ *runtime.ServeMux, _ runtime.Marshaler, w http.ResponseWriter, r *http.Request, err error) {
		st := status.Convert(err)
		body := ErrorBody{
			Code:      code.Code(st.Code()).String(),
			Message:   st.Message(),
			RequestID: logging.RequestID(r.Context()),
		}
		switch st.Code() {
		case codes.Unavailable:
			body.Message = "service unavailable, retry later"
		case codes.Internal, codes.Unknown, codes.DataLoss:
			body.Message = "internal error"
		}
		for _, d := range st.Proto().GetDetails() {
			if b, err := protojson.Marshal(d); err == nil {
				body.Details = append(body.Details, json.RawMessage(b))
			}
		}
		httpStatus := runtime.HTTPStatusFromCode(st.Code())
		if httpStatus >= http.StatusInternalServerError {
			log.ErrorContext(ctx, "gateway error", "code", st.Code().String(), "err", st.Message())
		}
		WriteError(w, httpStatus, body)
	}
}
