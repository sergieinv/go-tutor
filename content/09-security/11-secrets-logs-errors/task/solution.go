package task

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
)

const redacted = "[REDACTED]"

// Secret — секретное значение. Настоящее значение — только через Reveal.
type Secret string

// Reveal возвращает настоящее значение.
func (s Secret) Reveal() string { return string(s) }

// Format перехватывает все глаголы fmt, включая %#v и %d.
func (Secret) Format(f fmt.State, verb rune) { io.WriteString(f, redacted) }

// LogValue — для slog.
func (Secret) LogValue() slog.Value { return slog.StringValue(redacted) }

// MarshalJSON — для encoding/json и JSON-логов со структурами.
func (Secret) MarshalJSON() ([]byte, error) { return json.Marshal(redacted) }

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

var sensitiveKeys = map[string]bool{
	"password": true, "secret": true, "token": true,
	"authorization": true, "cookie": true, "api_key": true,
}

// NewLogger возвращает JSON-логгер, скрывающий значения опасных ключей.
func NewLogger(w io.Writer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(w, &slog.HandlerOptions{
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if sensitiveKeys[strings.ToLower(a.Key)] {
				return slog.String(a.Key, redacted)
			}
			return a
		},
	}))
}

// Handle превращает хендлер с ошибкой в http.Handler: id запроса,
// ошибки и паники — в лог, клиенту — без подробностей.
func Handle(log *slog.Logger, h func(w http.ResponseWriter, r *http.Request) error) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := rand.Text()
		w.Header().Set("X-Request-ID", id)
		reply := func(status int, msg string) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(status)
			json.NewEncoder(w).Encode(map[string]string{"error": msg, "request_id": id})
		}
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if v == http.ErrAbortHandler {
				panic(v)
			}
			log.Error("panic", "request_id", id, "panic", fmt.Sprint(v), "stack", string(debug.Stack()))
			reply(http.StatusInternalServerError, "internal error")
		}()

		err := h(w, r)
		if err == nil {
			return
		}
		log.Error("request failed", "request_id", id, "method", r.Method, "path", r.URL.Path, "err", err)
		var pe *PublicError
		if errors.As(err, &pe) {
			reply(pe.Status, pe.Msg)
			return
		}
		reply(http.StatusInternalServerError, "internal error")
	})
}
