package task

import (
	"context"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
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
	switch s {
	case Starting:
		return "starting"
	case Ready:
		return "ready"
	case Draining:
		return "draining"
	}
	return "unknown"
}

// Health — стадии и проверки зависимостей для проб Kubernetes.
type Health struct {
	timeout time.Duration

	mu      sync.Mutex
	stage   Stage
	started bool // MarkReady вызывался хотя бы раз
	checks  map[string]func(context.Context) error
}

func New(timeout time.Duration) *Health {
	if timeout <= 0 {
		timeout = time.Second
	}
	return &Health{timeout: timeout, checks: map[string]func(context.Context) error{}}
}

func (h *Health) AddCheck(name string, fn func(ctx context.Context) error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.checks[name] = fn
}

func (h *Health) MarkReady() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.stage == Starting {
		h.stage = Ready
		h.started = true
	}
}

func (h *Health) Drain() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.stage = Draining
}

func (h *Health) Stage() Stage {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.stage
}

func reply(w http.ResponseWriter, code int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	fmt.Fprintln(w, body)
}

func (h *Health) Live() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reply(w, http.StatusOK, "ok")
	})
}

func (h *Health) Startup() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		started := h.started
		h.mu.Unlock()
		if !started {
			reply(w, http.StatusServiceUnavailable, "starting")
			return
		}
		reply(w, http.StatusOK, "ok")
	})
}

func (h *Health) Ready() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h.mu.Lock()
		stage := h.stage
		checks := make(map[string]func(context.Context) error, len(h.checks))
		for name, fn := range h.checks {
			checks[name] = fn
		}
		h.mu.Unlock()
		if stage != Ready {
			reply(w, http.StatusServiceUnavailable, stage.String())
			return
		}
		if failed := h.run(r.Context(), checks); len(failed) > 0 {
			reply(w, http.StatusServiceUnavailable, "not ready: "+strings.Join(failed, ","))
			return
		}
		reply(w, http.StatusOK, "ok")
	})
}

// run запускает проверки параллельно с общим таймаутом и возвращает
// отсортированные имена упавших; зависшие проверки не дожидается.
func (h *Health) run(ctx context.Context, checks map[string]func(context.Context) error) []string {
	ctx, cancel := context.WithTimeout(ctx, h.timeout)
	defer cancel()
	type result struct {
		name string
		ok   bool
	}
	results := make(chan result, len(checks)) // буфер: опоздавшие не блокируются
	for name, fn := range checks {
		go func() { results <- result{name, fn(ctx) == nil} }()
	}
	pending := make(map[string]bool, len(checks))
	for name := range checks {
		pending[name] = true
	}
	var failed []string
	for len(pending) > 0 {
		select {
		case res := <-results:
			delete(pending, res.name)
			if !res.ok {
				failed = append(failed, res.name)
			}
		case <-ctx.Done():
			for name := range pending {
				failed = append(failed, name)
			}
			pending = nil
		}
	}
	slices.Sort(failed)
	return failed
}
