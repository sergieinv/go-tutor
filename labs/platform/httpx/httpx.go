// Package httpx — HTTP-сервер сервиса с безопасными настройками и
// middleware: id запроса, восстановление после паники, дедлайн обработки,
// журнал и RED-метрики (Rate, Errors, Duration) по шаблону маршрута.
//
// Middleware в Go — функция func(http.Handler) http.Handler, без
// IMiddleware и конвейера ASP.NET Core: цепочка собирается обычными
// вызовами в main, и её порядок виден в коде.
package httpx

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strconv"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"gotutor/labs/platform/health"
	"gotutor/labs/platform/logging"
)

// NewServer возвращает http.Server с таймаутами. Без них медленный клиент
// держит соединение и горутину сколько угодно (slowloris).
func NewServer(h http.Handler, log *slog.Logger) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}
}

// OpsHandler — служебный HTTP сервиса: /healthz, /readyz и /metrics для
// Prometheus. Слушает отдельный порт, который не публикуется наружу:
// метрики и пробы нужны оркестратору и мониторингу, а не клиентам.
func OpsHandler(reg prometheus.Gatherer, ready *health.Readiness, log *slog.Logger) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /healthz", health.LiveHandler())
	mux.Handle("GET /readyz", health.ReadyHandler(ready, log))
	mux.Handle("GET /metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{
		ErrorLog: slog.NewLogLogger(log.Handler(), slog.LevelWarn),
		// Не отдавать полуготовый ответ, если сбор метрик упал.
		ErrorHandling: promhttp.HTTPErrorOnError,
	}))
	return mux
}

// RequestID присваивает запросу id: он попадает в контекст (и оттуда — в
// журнал и в metadata исходящих gRPC-вызовов), в заголовок ответа
// X-Request-ID и в тело ошибок.
//
// Входящий X-Request-ID принимается, только если похож на id (см.
// logging.ValidRequestID): он попадёт в журнал как есть.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get("X-Request-ID")
		if !logging.ValidRequestID(id) {
			id = logging.NewRequestID()
		}
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(logging.WithRequestID(r.Context(), id)))
	})
}

// Timeout ограничивает время обработки: контекст запроса получает дедлайн,
// и он доходит до gRPC-вызовов и драйвера базы. http.Server.WriteTimeout так
// не умеет: он рвёт соединение, а обработчик продолжает работать.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), d)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// Recover превращает панику обработчика в ответ 500 без подробностей.
// Стек — в журнал вместе с request_id.
func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				v := recover()
				if v == nil {
					return
				}
				// ErrAbortHandler — штатный способ оборвать ответ.
				if v == http.ErrAbortHandler {
					panic(v)
				}
				log.ErrorContext(r.Context(), "panic", "panic", v, "stack", string(debug.Stack()))
				// Если ответ уже начат, заголовки не поменять: остаётся
				// оборвать соединение, чтобы клиент не принял огрызок за успех.
				if rec, ok := w.(*statusRecorder); ok && rec.status != 0 {
					panic(http.ErrAbortHandler)
				}
				WriteError(w, http.StatusInternalServerError, ErrorBody{
					Code:      "INTERNAL",
					Message:   "internal error",
					RequestID: logging.RequestID(r.Context()),
				})
			}()
			next.ServeHTTP(w, r)
		})
	}
}

// ErrorBody — тело ответа об ошибке: {"error": {...}}. Один формат для
// всех ошибок сервиса — и от grpc-gateway, и от middleware.
type ErrorBody struct {
	// Code — имя кода gRPC (NOT_FOUND, INVALID_ARGUMENT…): клиент ветвится
	// по нему, а не по тексту.
	Code    string `json:"code"`
	Message string `json:"message"`
	// RequestID — по нему ошибку находят в журнале.
	RequestID string `json:"request_id,omitempty"`
	// Details — детали google.rpc (BadRequest, ErrorInfo…) в protojson.
	Details []json.RawMessage `json:"details,omitempty"`
}

// WriteError пишет ErrorBody с кодом status.
func WriteError(w http.ResponseWriter, status int, body ErrorBody) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(struct {
		Error ErrorBody `json:"error"`
	}{body})
}

// Metrics — RED-метрики HTTP-сервера.
type Metrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

// NewMetrics регистрирует метрики в reg.
//
// Метки — метод, шаблон маршрута и код ответа: у каждой ограниченный
// набор значений. Настоящий путь (/v1/orders/42) или id покупателя в
// метке — взрыв кардинальности: по временному ряду на каждое значение.
func NewMetrics(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		requests: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "http_server_requests_total",
			Help: "HTTP requests by method, route template and status code.",
		}, []string{"method", "route", "code"}),
		duration: prometheus.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "http_server_request_duration_seconds",
			Help:    "HTTP request latency by method and route template.",
			Buckets: prometheus.DefBuckets,
		}, []string{"method", "route"}),
	}
	reg.MustRegister(m.requests, m.duration)
	return m
}

// Instrument пишет строку журнала и метрики на каждый запрос.
//
// Шаблон маршрута узнаётся после обработки: его записывает внутренний
// маршрутизатор через SetRoute (grpc-gateway) или net/http.ServeMux в
// r.Pattern. Не нашёлся — "unmatched".
func Instrument(log *slog.Logger, m *Metrics) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			route := &routeHolder{}
			r = r.WithContext(context.WithValue(r.Context(), routeKey{}, route))
			rec := &statusRecorder{ResponseWriter: w}

			next.ServeHTTP(rec, r)

			status := rec.status
			if status == 0 {
				status = http.StatusOK // обработчик ничего не записал
			}
			name := route.name
			if name == "" {
				name = r.Pattern
			}
			if name == "" {
				name = "unmatched"
			}
			elapsed := time.Since(start)
			if m != nil {
				m.requests.WithLabelValues(r.Method, name, strconv.Itoa(status)).Inc()
				m.duration.WithLabelValues(r.Method, name).Observe(elapsed.Seconds())
			}
			level := slog.LevelInfo
			if status >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			log.Log(r.Context(), level, "http request",
				"method", r.Method,
				"route", name,
				"status", status,
				"duration_ms", elapsed.Milliseconds(),
			)
		})
	}
}

type routeKey struct{}

type routeHolder struct{ name string }

// SetRoute сообщает Instrument шаблон маршрута текущего запроса. Вызывается
// из маршрутизатора, который знает шаблон (middleware grpc-gateway).
func SetRoute(ctx context.Context, route string) {
	if h, ok := ctx.Value(routeKey{}).(*routeHolder); ok {
		h.name = route
	}
}

// statusRecorder запоминает код ответа для журнала, метрик и Recover.
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
// (Flush, SetWriteDeadline).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
