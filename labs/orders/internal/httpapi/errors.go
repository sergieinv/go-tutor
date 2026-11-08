package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"net/http"

	"gotutor/labs/orders/internal/orders"
)

// requestError — ошибка разбора запроса на уровне HTTP (не JSON, не тот
// Content-Type, слишком большое тело, плохой id в пути).
type requestError struct {
	status  int
	code    string
	message string
	cause   error // подробности — только в журнал
}

func (e *requestError) Error() string {
	if e.cause != nil {
		return e.message + ": " + e.cause.Error()
	}
	return e.message
}

func (e *requestError) Unwrap() error { return e.cause }

// fail — единственное место, где ошибки превращаются в HTTP-ответы.
// Наружу уходят код и безопасное сообщение; текст исходной ошибки (SQL,
// имена таблиц, адреса) — только в журнал, вместе с id запроса.
func (h *handler) fail(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	resp := errorResponse{RequestID: requestID(ctx)}
	var status int

	var reqErr *requestError
	var valErr *orders.ValidationError
	switch {
	case errors.As(err, &reqErr):
		status, resp.Code, resp.Message = reqErr.status, reqErr.code, reqErr.message
	case errors.As(err, &valErr):
		// Поле и причина валидации — про данные самого клиента, их показывать можно.
		status, resp.Code, resp.Message, resp.Field = http.StatusUnprocessableEntity, "validation_failed", valErr.Message, valErr.Field
	case errors.Is(err, orders.ErrNotFound):
		status, resp.Code, resp.Message = http.StatusNotFound, "not_found", "resource not found"
	case errors.Is(err, orders.ErrOutOfStock):
		status, resp.Code, resp.Message = http.StatusConflict, "out_of_stock", "not enough stock"
	case errors.Is(err, orders.ErrConflict):
		status, resp.Code, resp.Message = http.StatusConflict, "conflict", "order state does not allow this operation"
	case errors.Is(err, context.DeadlineExceeded):
		// Сработал наш дедлайн (requestTimeout): база не успела.
		status, resp.Code, resp.Message = http.StatusServiceUnavailable, "timeout", "request timed out"
	case errors.Is(err, context.Canceled):
		// Клиент закрыл соединение — отвечать некому. Статус 499 (как у
		// nginx) попадёт только в журнал.
		status, resp.Code, resp.Message = 499, "canceled", "client closed request"
	default:
		status, resp.Code, resp.Message = http.StatusInternalServerError, "internal", "internal error"
	}

	level := slog.LevelInfo
	if status >= http.StatusInternalServerError {
		level = slog.LevelError
	}
	h.log.Log(ctx, level, "request failed",
		"request_id", resp.RequestID,
		"method", r.Method,
		"path", r.URL.Path,
		"status", status,
		"err", err,
	)
	writeJSON(w, status, resp)
}

// decodeJSON читает тело запроса в dst — строго:
//   - Content-Type обязан быть application/json;
//   - тело не больше maxBodyBytes (MaxBytesReader обрывает чтение и
//     закрывает соединение у слишком жадного клиента);
//   - неизвестные поля — ошибка (опечатка «quantiy» не превратится молча
//     в нулевое количество);
//   - после объекта ничего, кроме пробелов.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		return &requestError{status: http.StatusUnsupportedMediaType, code: "unsupported_media_type", message: "Content-Type must be application/json"}
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return &requestError{status: http.StatusRequestEntityTooLarge, code: "body_too_large", message: "request body too large", cause: err}
		}
		return &requestError{status: http.StatusBadRequest, code: "invalid_json", message: "request body is not a valid JSON object of the expected shape", cause: err}
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return &requestError{status: http.StatusBadRequest, code: "invalid_json", message: "request body must contain a single JSON object", cause: err}
	}
	return nil
}

// writeJSON пишет ответ. Ошибку записи не возвращаем: заголовки уже ушли,
// исправить ответ нельзя, а обрыв соединения клиентом — не ошибка сервиса.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
