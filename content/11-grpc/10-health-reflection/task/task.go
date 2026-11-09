package task

import (
	"context"
	"time"

	"google.golang.org/grpc/health"
)

// Dependency — внешняя зависимость: база, брокер, соседний сервис.
type Dependency struct {
	Name  string
	Check func(ctx context.Context) error
}

// Monitor выставляет статусы health-сервера по состоянию зависимостей.
type Monitor struct {
	// TODO: поля
}

// NewMonitor: services — имя gRPC-сервиса → имена нужных ему зависимостей,
// timeout — лимит на одну проверку.
func NewMonitor(hs *health.Server, deps []Dependency, services map[string][]string, timeout time.Duration) *Monitor {
	// TODO
	return &Monitor{}
}

// Refresh один раз проверяет все зависимости, выставляет статусы и
// возвращает отсортированные имена упавших зависимостей (nil — все живы).
func (m *Monitor) Refresh(ctx context.Context) []string {
	// TODO
	return nil
}

// Run сразу вызывает Refresh, затем — на каждый сигнал tick, пока ctx не отменён
// и tick не закрыт.
func (m *Monitor) Run(ctx context.Context, tick <-chan time.Time) {
	// TODO
}
