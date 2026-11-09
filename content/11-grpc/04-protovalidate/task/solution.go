package task

import (
	"cmp"
	"context"
	"errors"
	"slices"

	"buf.build/go/protovalidate"
	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
)

// UnaryValidator проверяет каждый запрос по правилам protovalidate
// до вызова обработчика.
func UnaryValidator(v protovalidate.Validator) grpc.UnaryServerInterceptor {
	return func(ctx context.Context, req any, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (any, error) {
		msg, ok := req.(proto.Message)
		if !ok {
			return handler(ctx, req)
		}
		err := v.Validate(msg)
		if err == nil {
			return handler(ctx, req)
		}
		var verr *protovalidate.ValidationError
		if !errors.As(err, &verr) {
			// Ошибка в правилах — проблема сервера; детали наружу не отдаём.
			return nil, status.Error(codes.Internal, "internal error")
		}
		st, derr := status.New(codes.InvalidArgument, "invalid request").
			WithDetails(&errdetails.BadRequest{FieldViolations: violations(verr)})
		if derr != nil {
			return nil, status.Error(codes.Internal, "internal error")
		}
		return nil, st.Err()
	}
}

func violations(verr *protovalidate.ValidationError) []*errdetails.BadRequest_FieldViolation {
	out := make([]*errdetails.BadRequest_FieldViolation, 0, len(verr.Violations))
	for _, v := range verr.Violations {
		out = append(out, &errdetails.BadRequest_FieldViolation{
			Field:       protovalidate.FieldPathString(v.Proto.GetField()),
			Description: v.Proto.GetMessage(),
			Reason:      v.Proto.GetRuleId(),
		})
	}
	slices.SortFunc(out, func(a, b *errdetails.BadRequest_FieldViolation) int {
		return cmp.Or(
			cmp.Compare(a.GetField(), b.GetField()),
			cmp.Compare(a.GetReason(), b.GetReason()),
			cmp.Compare(a.GetDescription(), b.GetDescription()),
		)
	})
	return out
}
