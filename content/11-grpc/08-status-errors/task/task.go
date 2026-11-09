package task

import (
	"errors"
	"time"

	"google.golang.org/grpc/status"
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

// ToStatus переводит ошибку домена в статус gRPC.
func ToStatus(err error) *status.Status {
	// TODO
	return nil
}

// ShouldRetry решает по ошибке вызова, повторять ли его и через сколько.
func ShouldRetry(err error) (bool, time.Duration) {
	// TODO
	return false, 0
}
