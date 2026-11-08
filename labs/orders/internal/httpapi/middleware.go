package httpapi

import (
	"context"
	"crypto/rand"
	"net/http"
	"runtime/debug"
	"time"
)

type ctxKey struct{}

// withRequestID присваивает запросу id: он попадает в контекст (и оттуда
// в журнал), в заголовок ответа X-Request-ID и в тело ошибок.
//
// Входящий X-Request-ID не принимаем: это непроверенные данные клиента,
// которые попали бы в журнал как есть. За балансировщиком, который сам
// ставит id, его можно принимать после проверки формата.
func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := rand.Text()
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func requestID(ctx context.Context) string {
	id, _ := ctx.Value(ctxKey{}).(string)
	return id
}

// statusRecorder запоминает статус ответа для журнала и для recover
// (можно ли ещё записать заголовки).
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(status int) {
	if r.status == 0 {
		r.status = status
	}
	r.ResponseWriter.WriteHeader(status)
}

func (r *statusRecorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap даёт http.ResponseController добраться до исходного writer
// (Flush, SetWriteDeadline и т.п.).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests пишет строку журнала на каждый запрос. Путь — шаблон
// маршрута (r.Pattern), а не URL: так у метрик и журнала ограниченный
// набор значений, и в журнал не попадают произвольные строки из URL.
func (h *handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		h.log.InfoContext(r.Context(), "request",
			"request_id", requestID(r.Context()),
			"method", r.Method,
			"pattern", r.Pattern,
			"status", rec.status,
			"duration", time.Since(start),
		)
	})
}

// recoverPanic превращает панику в обработчике в ответ 500. Стек — в
// журнал, клиенту — только код ошибки и id запроса.
func (h *handler) recoverPanic(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			// ErrAbortHandler — штатный способ оборвать ответ; net/http
			// обработает его сам и не станет печатать стек.
			if v == http.ErrAbortHandler {
				panic(v)
			}
			h.log.ErrorContext(r.Context(), "panic",
				"request_id", requestID(r.Context()),
				"panic", v,
				"stack", string(debug.Stack()),
			)
			// Если ответ уже начат, заголовки не поменять: остаётся
			// оборвать соединение, чтобы клиент не принял огрызок за успех.
			if rec, ok := w.(*statusRecorder); ok && rec.status != 0 {
				panic(http.ErrAbortHandler)
			}
			writeJSON(w, http.StatusInternalServerError, errorResponse{
				Code:      "internal",
				Message:   "internal error",
				RequestID: requestID(r.Context()),
			})
		}()
		next.ServeHTTP(w, r)
	})
}

// withTimeout ограничивает время обработки: контекст запроса получает
// дедлайн, и запросы к базе, выполняемые с этим контекстом, прервутся.
// http.Server.WriteTimeout так не умеет: он закрывает соединение, но
// обработчик и его SQL продолжают работать.
func withTimeout(next http.Handler, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
