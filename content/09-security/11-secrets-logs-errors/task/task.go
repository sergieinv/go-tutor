package task

import (
	"io"
	"log/slog"
	"net/http"
)

// Secret — секретное значение. Настоящее значение — только через Reveal.
type Secret string

// Reveal возвращает настоящее значение.
func (s Secret) Reveal() string { return string(s) }

// TODO: добавьте методы, чтобы fmt, slog и encoding/json печатали "[REDACTED]".

// PublicError — ошибка, текст Msg которой можно показать клиенту.
// Err — внутренняя причина, только для лога.
type PublicError struct {
	Status int
	Msg    string
	Err    error
}

func (e *PublicError) Error() string {
	if e.Err == nil {
		return e.Msg
	}
	return e.Msg + ": " + e.Err.Error()
}

func (e *PublicError) Unwrap() error { return e.Err }

// NewLogger возвращает JSON-логгер, скрывающий значения опасных ключей.
func NewLogger(w io.Writer) *slog.Logger {
	// TODO: реализуйте функцию
	return slog.New(slog.NewJSONHandler(w, nil))
}

// Handle превращает хендлер с ошибкой в http.Handler: id запроса,
// ошибки и паники — в лог, клиенту — без подробностей.
func Handle(log *slog.Logger, h func(w http.ResponseWriter, r *http.Request) error) http.Handler {
	// TODO: реализуйте функцию
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
}
