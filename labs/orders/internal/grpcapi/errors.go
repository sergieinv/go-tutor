package grpcapi

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/durationpb"

	"gotutor/labs/orders/internal/orders"
	"gotutor/labs/platform/grpcx"
)

// errorDomain — домен ErrorInfo «Заказов».
const errorDomain = "orders.gotutor.local"

// toStatus — единственное место, где доменные ошибки становятся статусами
// gRPC (а через grpc-gateway — HTTP-кодами). Наружу — код, безопасное
// сообщение и структурированные детали; текст исходной ошибки (SQL,
// адреса) — только в журнал (grpcx.Internal и interceptor журнала).
func (s *Server) toStatus(ctx context.Context, err error) error {
	var valErr *orders.ValidationError
	var stockErr *orders.OutOfStockError
	switch {
	case errors.As(err, &valErr):
		return grpcx.InvalidArgument("invalid request", &errdetails.BadRequest_FieldViolation{
			Field:       valErr.Field,
			Description: valErr.Message,
		})
	case errors.As(err, &stockErr):
		return outOfStock(stockErr)
	case errors.Is(err, orders.ErrProductNotFound):
		return status.Error(codes.NotFound, "product not found")
	case errors.Is(err, orders.ErrNotFound):
		return status.Error(codes.NotFound, "order not found")
	case errors.Is(err, orders.ErrConflict):
		// Состояние поменялось параллельно: повтор всего запроса решит.
		return status.Error(codes.Aborted, "order was changed concurrently, retry")
	case errors.Is(err, orders.ErrWarehouseUnavailable):
		st := status.New(codes.Unavailable, "warehouse unavailable, retry later")
		// RetryInfo — подсказка клиенту, когда повторять.
		if d, derr := st.WithDetails(&errdetails.RetryInfo{RetryDelay: durationpb.New(time.Second)}); derr == nil {
			st = d
		}
		return st.Err()
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, "deadline exceeded")
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, "request canceled")
	}
	return grpcx.Internal(ctx, s.log, "request failed", err)
}

// outOfStock — FAILED_PRECONDITION с ErrorInfo (машиночитаемая причина) и
// PreconditionFailure (какие товары).
func outOfStock(e *orders.OutOfStockError) error {
	ids := make([]string, len(e.ProductIDs))
	pf := &errdetails.PreconditionFailure{}
	for i, id := range e.ProductIDs {
		ids[i] = strconv.FormatInt(id, 10)
		pf.Violations = append(pf.Violations, &errdetails.PreconditionFailure_Violation{
			Type:        "STOCK",
			Subject:     "products/" + ids[i],
			Description: "not enough stock",
		})
	}
	st := status.New(codes.FailedPrecondition, "not enough stock")
	details := []protoadapt.MessageV1{
		&errdetails.ErrorInfo{
			Reason:   "OUT_OF_STOCK",
			Domain:   errorDomain,
			Metadata: map[string]string{"product_ids": strings.Join(ids, ",")},
		},
	}
	if len(pf.Violations) > 0 {
		details = append(details, pf)
	}
	if d, err := st.WithDetails(details...); err == nil {
		st = d
	}
	return st.Err()
}
