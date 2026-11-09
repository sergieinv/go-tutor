// Package health — живость (liveness) и готовность (readiness) сервиса для
// оркестратора и балансировщика.
//
//   - /healthz — «процесс жив и обслуживает HTTP». Зависимости не
//     проверяет: если база упала, перезапуск сервиса её не поднимет, а
//     одновременный рестарт всех реплик только добавит аварии.
//   - /readyz — «можно слать трафик»: зависимости доступны и сервис не
//     останавливается. Перед остановкой готовность снимается первой, чтобы
//     балансировщик успел убрать реплику до закрытия соединений.
//
// В .NET то же дают AddHealthChecks() с тегами live/ready.
package health

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// Check проверяет одну зависимость (пинг базы, метаданные брокера).
type Check func(ctx context.Context) error

// ErrDraining — сервис останавливается и новый трафик не принимает.
var ErrDraining = errors.New("shutting down")

// Readiness — набор проверок готовности. Безопасен для конкурентного
// использования.
type Readiness struct {
	// Timeout — на все проверки одного запроса; 0 — 2 секунды. Зависшая
	// проверка не должна подвешивать пробу.
	Timeout time.Duration

	mu       sync.Mutex
	checks   []namedCheck
	draining atomic.Bool
}

type namedCheck struct {
	name  string
	check Check
}

// Add регистрирует проверку. Вызывается при сборке сервиса в main.
func (r *Readiness) Add(name string, c Check) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.checks = append(r.checks, namedCheck{name, c})
}

// SetDraining снимает готовность навсегда — первый шаг остановки.
func (r *Readiness) SetDraining() { r.draining.Store(true) }

// Check выполняет все проверки и возвращает их ошибки вместе.
func (r *Readiness) Check(ctx context.Context) error {
	if r.draining.Load() {
		return ErrDraining
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 2 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r.mu.Lock()
	checks := append([]namedCheck(nil), r.checks...)
	r.mu.Unlock()

	var errs []error
	for _, c := range checks {
		if err := c.check(ctx); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", c.name, err))
		}
	}
	return errors.Join(errs...)
}

// LiveHandler отвечает 200 на любой GET: процесс жив.
func LiveHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStatus(w, http.StatusOK, "ok")
	})
}

// ReadyHandler отвечает 200, если все проверки прошли, иначе 503. Причина
// отказа — только в журнал: наружу не уходят адреса и тексты ошибок
// зависимостей.
func ReadyHandler(r *Readiness, log *slog.Logger) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if err := r.Check(req.Context()); err != nil {
			if !errors.Is(err, ErrDraining) {
				log.WarnContext(req.Context(), "not ready", "err", err)
			}
			writeStatus(w, http.StatusServiceUnavailable, "unavailable")
			return
		}
		writeStatus(w, http.StatusOK, "ok")
	})
}

func writeStatus(w http.ResponseWriter, code int, status string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"status": status})
}

// GRPCStatusSetter — то, что SyncGRPC нужно от *health.Server из
// google.golang.org/grpc/health.
type GRPCStatusSetter interface {
	SetServingStatus(service string, status healthpb.HealthCheckResponse_ServingStatus)
}

// SyncGRPC раз в interval переносит готовность в статус gRPC health
// (grpc.health.v1) для services ("" — сервер целиком). По нему делают
// gRPC-пробы Kubernetes и grpc-клиенты с health checking. Работает до
// отмены ctx. При остановке main вызывает Shutdown у health-сервера: тот
// переводит всё в NOT_SERVING и игнорирует дальнейшие обновления.
func SyncGRPC(ctx context.Context, r *Readiness, srv GRPCStatusSetter, interval time.Duration, services ...string) {
	update := func() {
		status := healthpb.HealthCheckResponse_SERVING
		if r.Check(ctx) != nil {
			status = healthpb.HealthCheckResponse_NOT_SERVING
		}
		for _, s := range services {
			srv.SetServingStatus(s, status)
		}
	}
	update()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			update()
		}
	}
}
