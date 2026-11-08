package task

import (
	"errors"
	"net/http"
)

// ErrBadOrigin — недопустимая запись в списке origin.
var ErrBadOrigin = errors.New("bad origin")

var errNotImplemented = errors.New("not implemented")

// NewCORS возвращает CORS-middleware для списка разрешённых origin.
func NewCORS(origins []string) (func(http.Handler) http.Handler, error) {
	// TODO: реализуйте функцию
	return nil, errNotImplemented
}

// Protect оборачивает API защитой от CSRF и CORS-middleware.
func Protect(origins []string, next http.Handler) (http.Handler, error) {
	// TODO: реализуйте функцию
	return nil, errNotImplemented
}
