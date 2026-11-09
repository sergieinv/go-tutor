package task

import (
	"context"

	"buf.build/go/protovalidate"
	"google.golang.org/grpc"
)

// UnaryValidator проверяет каждый запрос по правилам protovalidate
// до вызова обработчика.
func UnaryValidator(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		// TODO: проверьте req и переведите нарушения в InvalidArgument + BadRequest.
		return handler(ctx, req)
	}
}
