package task

import (
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 9, 12, 0, 0, 0, time.UTC)

// clock — управляемые часы: обработчик «тратит» время, сдвигая их.
type clock struct {
	mu    sync.Mutex
	t     time.Time
	calls int
}

func newClock() *clock { return &clock{t: t0} }

func (c *clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.t
}

func (c *clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// handlerTaking — обработчик, который «работает» d по часам clk.
func handlerTaking(clk *clock, d time.Duration) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clk.Advance(d)
		w.Header().Set("X-Handler", "orders")
		w.WriteHeader(http.StatusCreated)
		io.WriteString(w, "ok")
	})
}

// snapshots записывает причины снимков.
type snapshots struct {
	mu      sync.Mutex
	reasons []string
}

func (s *snapshots) take(reason string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.reasons = append(s.reasons, reason)
	return nil
}

func (s *snapshots) list() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.reasons...)
}

func serve(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestSlowRequestCaptured(t *testing.T) {
	clk, snaps := newClock(), &snapshots{}
	c := &Capture{Threshold: time.Second, Cooldown: time.Minute, Now: clk.Now, Snapshot: snaps.take}
	rec := serve(c.Middleware(handlerTaking(clk, 2*time.Second)), "GET", "/orders/7")
	if rec.Code != http.StatusCreated || rec.Body.String() != "ok" || rec.Header().Get("X-Handler") != "orders" {
		t.Fatalf("ответ изменён: %d %q %v", rec.Code, rec.Body.String(), rec.Header())
	}
	got := snaps.list()
	if len(got) != 1 || got[0] != "GET /orders/7 2s" {
		t.Fatalf("снимки = %q, want [\"GET /orders/7 2s\"]", got)
	}
	if n := c.Captured(); n != 1 {
		t.Fatalf("Captured() = %d, want 1", n)
	}
}

func TestThresholdBoundary(t *testing.T) {
	clk, snaps := newClock(), &snapshots{}
	c := &Capture{Threshold: time.Second, Now: clk.Now, Snapshot: snaps.take}
	serve(c.Middleware(handlerTaking(clk, 999*time.Millisecond)), "GET", "/fast")
	if got := snaps.list(); len(got) != 0 {
		t.Fatalf("быстрый запрос (999ms) дал снимки %q", got)
	}
	serve(c.Middleware(handlerTaking(clk, time.Second)), "GET", "/edge")
	if got := snaps.list(); len(got) != 1 || got[0] != "GET /edge 1s" {
		t.Fatalf("запрос ровно на пороге: снимки %q, want [\"GET /edge 1s\"]", got)
	}
}

func TestCooldown(t *testing.T) {
	clk, snaps := newClock(), &snapshots{}
	c := &Capture{Threshold: time.Second, Cooldown: 10 * time.Second, Now: clk.Now, Snapshot: snaps.take}
	slow := c.Middleware(handlerTaking(clk, 2*time.Second))

	serve(slow, "GET", "/a") // попытка в t0+2s
	serve(slow, "GET", "/b") // t0+4s: прошло 2s < 10s — пропуск
	clk.Advance(6 * time.Second)
	serve(slow, "GET", "/c") // t0+12s: прошло ровно 10s — снимок
	serve(slow, "GET", "/d") // t0+14s: прошло 2s — пропуск
	got := snaps.list()
	if len(got) != 2 || got[0] != "GET /a 2s" || got[1] != "GET /c 2s" {
		t.Fatalf("снимки = %q, want [GET /a 2s, GET /c 2s]", got)
	}
	if n := c.Captured(); n != 2 {
		t.Fatalf("Captured() = %d, want 2", n)
	}
}

func TestThresholdZeroDisables(t *testing.T) {
	clk, snaps := newClock(), &snapshots{}
	c := &Capture{Threshold: 0, Now: clk.Now, Snapshot: snaps.take}
	serve(c.Middleware(handlerTaking(clk, time.Hour)), "GET", "/x")
	if got := snaps.list(); len(got) != 0 || c.Captured() != 0 {
		t.Fatalf("Threshold = 0: снимки %q, Captured %d", got, c.Captured())
	}
}
