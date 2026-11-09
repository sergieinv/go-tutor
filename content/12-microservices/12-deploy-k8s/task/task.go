package task

import (
	"context"
	"net/http"
	"time"
)

// Stage — стадия жизни пода.
type Stage int

const (
	Starting Stage = iota
	Ready
	Draining
)

func (s Stage) String() string {
	// TODO: "starting", "ready", "draining"
	return ""
}

// Health — стадии и проверки зависимостей для проб Kubernetes.
type Health struct {
	// TODO
}

func New(timeout time.Duration) *Health {
	// TODO
	return &Health{}
}

func (h *Health) AddCheck(name string, fn func(ctx context.Context) error) {
	// TODO
}

func (h *Health) MarkReady() {
	// TODO
}

func (h *Health) Drain() {
	// TODO
}

func (h *Health) Stage() Stage {
	// TODO
	return Starting
}

func (h *Health) Live() http.Handler {
	// TODO
	return http.NotFoundHandler()
}

func (h *Health) Startup() http.Handler {
	// TODO
	return http.NotFoundHandler()
}

func (h *Health) Ready() http.Handler {
	// TODO
	return http.NotFoundHandler()
}
