package task

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"time"

	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

// Dependency — внешняя зависимость: база, брокер, соседний сервис.
type Dependency struct {
	Name  string
	Check func(ctx context.Context) error
}

// Monitor выставляет статусы health-сервера по состоянию зависимостей.
type Monitor struct {
	hs       *health.Server
	deps     []Dependency
	services map[string][]string
	timeout  time.Duration
}

// NewMonitor: services — имя gRPC-сервиса → имена нужных ему зависимостей,
// timeout — лимит на одну проверку.
func NewMonitor(hs *health.Server, deps []Dependency, services map[string][]string, timeout time.Duration) *Monitor {
	return &Monitor{hs: hs, deps: deps, services: services, timeout: timeout}
}

// Refresh один раз проверяет все зависимости, выставляет статусы и
// возвращает отсортированные имена упавших зависимостей (nil — все живы).
func (m *Monitor) Refresh(ctx context.Context) []string {
	ok := make(map[string]bool, len(m.deps))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, d := range m.deps {
		wg.Go(func() {
			err := m.check(ctx, d)
			mu.Lock()
			ok[d.Name] = err == nil
			mu.Unlock()
		})
	}
	wg.Wait()

	failed := map[string]bool{}
	for name, alive := range ok {
		if !alive {
			failed[name] = true
		}
	}
	all := healthpb.HealthCheckResponse_SERVING
	for svc, need := range m.services {
		st := healthpb.HealthCheckResponse_SERVING
		for _, name := range need {
			if !ok[name] { // неизвестное имя тоже сюда: ok[name] == false
				failed[name] = true
				st = healthpb.HealthCheckResponse_NOT_SERVING
			}
		}
		if st != healthpb.HealthCheckResponse_SERVING {
			all = st
		}
		m.hs.SetServingStatus(svc, st)
	}
	m.hs.SetServingStatus("", all)

	var names []string
	for name := range failed {
		names = append(names, name)
	}
	slices.Sort(names)
	return names
}

// check выполняет одну проверку со своим таймаутом; паника — тоже сбой.
func (m *Monitor) check(ctx context.Context, d Dependency) (err error) {
	ctx, cancel := context.WithTimeout(ctx, m.timeout)
	defer cancel()
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return d.Check(ctx)
}

// Run сразу вызывает Refresh, затем — на каждый сигнал tick, пока ctx не отменён
// и tick не закрыт.
func (m *Monitor) Run(ctx context.Context, tick <-chan time.Time) {
	m.Refresh(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case _, ok := <-tick:
			if !ok {
				return
			}
			m.Refresh(ctx)
		}
	}
}
