package grpcapi

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"

	warehousev1 "gotutor/labs/api/gen/warehouse/v1"
	"gotutor/labs/platform/grpcx"
	"gotutor/labs/warehouse/internal/stock"
)

// ErrorDomain — google.rpc.ErrorInfo.domain ошибок «Склада».
const ErrorDomain = "warehouse.gotutor.local"

// Reason возвращает значение ErrorInfo.reason для значения enum'а контракта:
// имя без префикса, "OUT_OF_STOCK" для ERROR_REASON_OUT_OF_STOCK. Имя
// берётся из сгенерированного кода, а не пишется строкой второй раз.
func Reason(r warehousev1.ErrorReason) string {
	return strings.TrimPrefix(r.String(), "ERROR_REASON_")
}

// toStatus — единственное место, где доменные ошибки становятся статусами
// gRPC. Клиент ветвится по коду и ErrorInfo.reason, а не по тексту
// сообщения; сообщения короткие и безопасные. Всё неизвестное — INTERNAL
// без подробностей, оригинал — в журнал (grpcx.Internal).
func (s *Server) toStatus(ctx context.Context, op string, err error) error {
	var oos *stock.OutOfStockError
	switch {
	case errors.As(err, &oos):
		pf := &errdetails.PreconditionFailure{}
		for _, sh := range oos.Shortages {
			pf.Violations = append(pf.Violations, &errdetails.PreconditionFailure_Violation{
				Type:        "STOCK",
				Subject:     fmt.Sprintf("products/%d", sh.ProductID),
				Description: fmt.Sprintf("requested %d, available %d", sh.Requested, sh.Available),
			})
		}
		return withDetails(codes.FailedPrecondition, "insufficient stock",
			errorInfo(warehousev1.ErrorReason_ERROR_REASON_OUT_OF_STOCK), pf)
	case errors.Is(err, stock.ErrReservationReleased):
		return withDetails(codes.Aborted, "reservation for this order is already released",
			errorInfo(warehousev1.ErrorReason_ERROR_REASON_RESERVATION_RELEASED))
	case errors.Is(err, stock.ErrReservationMismatch):
		return withDetails(codes.AlreadyExists, "order already has a reservation with different items",
			errorInfo(warehousev1.ErrorReason_ERROR_REASON_RESERVATION_MISMATCH))
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		// Клиент ушёл или истёк дедлайн — не ошибка сервера: DEADLINE_EXCEEDED
		// или CANCELED, а не INTERNAL.
		return status.FromContextError(err).Err()
	}
	return grpcx.Internal(ctx, s.log, op, err)
}

func errorInfo(r warehousev1.ErrorReason) *errdetails.ErrorInfo {
	return &errdetails.ErrorInfo{Domain: ErrorDomain, Reason: Reason(r)}
}

// withDetails собирает статус с деталями google.rpc (они едут клиенту в
// trailer grpc-status-details-bin и разворачиваются через status.Details).
func withDetails(c codes.Code, msg string, details ...protoadapt.MessageV1) error {
	st := status.New(c, msg)
	if d, err := st.WithDetails(details...); err == nil {
		st = d
	}
	return st.Err()
}
