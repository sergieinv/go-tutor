package task

import (
	"context"
	"errors"
	"time"

	"google.golang.org/genproto/googleapis/rpc/errdetails"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/protoadapt"
	"google.golang.org/protobuf/types/known/durationpb"
)

// Доменные ошибки сервиса заказов.
var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
	ErrConflict      = errors.New("conflict")
)

// Domain — домен для ErrorInfo.
const Domain = "orders.example.com"

// FieldViolation — ошибка в одном поле запроса.
type FieldViolation struct{ Field, Description string }

// ValidationError — запрос не прошёл проверку.
type ValidationError struct{ Violations []FieldViolation }

func (e *ValidationError) Error() string { return "validation failed" }

// PreconditionError — состояние системы не позволяет выполнить запрос.
type PreconditionError struct{ Type, Subject, Description string }

func (e *PreconditionError) Error() string { return "precondition failed: " + e.Description }

// UnavailableError — сервис временно недоступен.
type UnavailableError struct{ RetryAfter time.Duration }

func (e *UnavailableError) Error() string { return "unavailable" }

func withDetails(c codes.Code, msg string, details ...protoadapt.MessageV1) *status.Status {
	st := status.New(c, msg)
	if len(details) == 0 {
		return st
	}
	if d, err := st.WithDetails(details...); err == nil {
		return d
	}
	return st
}

func info(reason string) *errdetails.ErrorInfo {
	return &errdetails.ErrorInfo{Reason: reason, Domain: Domain}
}

// ToStatus переводит ошибку домена в статус gRPC.
func ToStatus(err error) *status.Status {
	if err == nil {
		return nil
	}
	var (
		ve *ValidationError
		pe *PreconditionError
		ue *UnavailableError
	)
	switch {
	case errors.Is(err, context.Canceled):
		return status.New(codes.Canceled, "canceled")
	case errors.Is(err, context.DeadlineExceeded):
		return status.New(codes.DeadlineExceeded, "deadline exceeded")
	case errors.As(err, &ve):
		br := &errdetails.BadRequest{}
		for _, v := range ve.Violations {
			br.FieldViolations = append(br.FieldViolations,
				&errdetails.BadRequest_FieldViolation{Field: v.Field, Description: v.Description})
		}
		return withDetails(codes.InvalidArgument, "invalid request", br)
	case errors.As(err, &pe):
		return withDetails(codes.FailedPrecondition, "precondition failed", &errdetails.PreconditionFailure{
			Violations: []*errdetails.PreconditionFailure_Violation{
				{Type: pe.Type, Subject: pe.Subject, Description: pe.Description},
			},
		})
	case errors.Is(err, ErrNotFound):
		return withDetails(codes.NotFound, "not found", info("NOT_FOUND"))
	case errors.Is(err, ErrAlreadyExists):
		return withDetails(codes.AlreadyExists, "already exists", info("ALREADY_EXISTS"))
	case errors.Is(err, ErrConflict):
		return withDetails(codes.Aborted, "conflict", info("CONFLICT"))
	case errors.As(err, &ue):
		if ue.RetryAfter > 0 {
			return withDetails(codes.Unavailable, "temporarily unavailable",
				&errdetails.RetryInfo{RetryDelay: durationpb.New(ue.RetryAfter)})
		}
		return status.New(codes.Unavailable, "temporarily unavailable")
	default:
		// Текст err может содержать SQL, адреса, имена — наружу его не отдаём.
		return status.New(codes.Internal, "internal error")
	}
}

// retryDelay возвращает задержку из первого RetryInfo, если она валидна.
func retryDelay(st *status.Status) (time.Duration, bool) {
	for _, d := range st.Details() {
		ri, ok := d.(*errdetails.RetryInfo)
		if !ok {
			continue
		}
		rd := ri.GetRetryDelay()
		if rd == nil || rd.CheckValid() != nil || rd.AsDuration() < 0 {
			return 0, false
		}
		return rd.AsDuration(), true
	}
	return 0, false
}

// ShouldRetry решает по ошибке вызова, повторять ли его и через сколько.
func ShouldRetry(err error) (bool, time.Duration) {
	st, ok := status.FromError(err)
	if err == nil || !ok {
		return false, 0
	}
	delay, hasInfo := retryDelay(st)
	switch st.Code() {
	case codes.Unavailable, codes.Aborted:
		return true, delay
	case codes.ResourceExhausted:
		if hasInfo {
			return true, delay
		}
	}
	return false, 0
}
