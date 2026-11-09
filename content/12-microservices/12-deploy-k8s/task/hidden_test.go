package task

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestHiddenFailedNamesSortedNoDetails(t *testing.T) {
	h := New(time.Second)
	secret := errors.New("password authentication failed for user orders (pass=hunter2)")
	for _, name := range []string{"redis", "db", "kafka", "s3"} {
		ok := name == "s3"
		h.AddCheck(name, func(context.Context) error {
			if ok {
				return nil
			}
			return secret
		})
	}
	h.MarkReady()
	code, body := get(t, h.Ready())
	if code != 503 || body != "not ready: db,kafka,redis\n" {
		t.Errorf("ready = %d %q, want 503 %q", code, body, "not ready: db,kafka,redis\n")
	}
	if strings.Contains(body, "hunter2") || strings.Contains(body, "password") {
		t.Error("текст ошибки проверки утёк в ответ пробы")
	}
}

func TestHiddenContentType(t *testing.T) {
	h := New(time.Second)
	for name, handler := range map[string]http.Handler{"live": h.Live(), "startup": h.Startup(), "ready": h.Ready()} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
			t.Errorf("%s: Content-Type = %q, want text/plain", name, ct)
		}
	}
}

func TestHiddenHangingCheckTimesOut(t *testing.T) {
	h := New(50 * time.Millisecond)
	block := make(chan struct{})
	defer close(block)
	h.AddCheck("db", func(context.Context) error { <-block; return nil }) // не смотрит на ctx
	h.AddCheck("cache", func(context.Context) error { return nil })
	h.MarkReady()
	done := make(chan string, 1)
	go func() {
		code, body := get(t, h.Ready())
		done <- fmt.Sprint(code, " ", body)
	}()
	select {
	case got := <-done:
		if got != "503 not ready: db\n" {
			t.Errorf("ready = %q, want %q", got, "503 not ready: db\n")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Ready ждёт зависшую проверку дольше таймаута")
	}
}

func TestHiddenCheckContext(t *testing.T) {
	type key struct{}
	h := New(300 * time.Millisecond)
	var left atomic.Int64
	var val atomic.Value
	h.AddCheck("db", func(ctx context.Context) error {
		dl, ok := ctx.Deadline()
		if !ok {
			return errors.New("нет дедлайна")
		}
		left.Store(int64(time.Until(dl)))
		val.Store(fmt.Sprint(ctx.Value(key{})))
		return nil
	})
	h.MarkReady()
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), key{}, "probe-1"))
	rec := httptest.NewRecorder()
	h.Ready().ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("ready = %d %q", rec.Code, rec.Body.String())
	}
	if d := time.Duration(left.Load()); d <= 0 || d > 300*time.Millisecond {
		t.Errorf("остаток до дедлайна проверки %v, want в (0, 300ms]", d)
	}
	if val.Load() != "probe-1" {
		t.Errorf("контекст проверки не потомок контекста запроса: значение %v", val.Load())
	}
}

func TestHiddenDefaultTimeout(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		h := New(d)
		var left atomic.Int64
		h.AddCheck("db", func(ctx context.Context) error {
			dl, _ := ctx.Deadline()
			left.Store(int64(time.Until(dl)))
			return nil
		})
		h.MarkReady()
		get(t, h.Ready())
		if got := time.Duration(left.Load()); got <= 500*time.Millisecond || got > time.Second {
			t.Errorf("New(%v): остаток %v, want ≈ 1s", d, got)
		}
	}
}

func TestHiddenChecksRunConcurrently(t *testing.T) {
	h := New(2 * time.Second)
	const n = 3
	var started sync.WaitGroup
	started.Add(n)
	all := make(chan struct{})
	go func() { started.Wait(); close(all) }()
	for i := range n {
		h.AddCheck(fmt.Sprint("dep", i), func(ctx context.Context) error {
			started.Done()
			select {
			case <-all:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}
	h.MarkReady()
	expect(t, "ready", h.Ready(), 200, "ok\n")
}

func TestHiddenNoChecksOutsideReady(t *testing.T) {
	h := New(time.Second)
	var calls atomic.Int32
	h.AddCheck("db", func(context.Context) error { calls.Add(1); return nil })
	expect(t, "ready (starting)", h.Ready(), 503, "starting\n")
	h.MarkReady()
	h.Drain()
	expect(t, "ready (draining)", h.Ready(), 503, "draining\n")
	if calls.Load() != 0 {
		t.Errorf("проверки вызваны %d раз вне стадии ready", calls.Load())
	}
}

func TestHiddenTransitionsOnlyForward(t *testing.T) {
	h := New(time.Second)
	h.Drain() // остановка во время старта
	if h.Stage() != Draining {
		t.Fatalf("Drain из starting: Stage = %v, want draining", h.Stage())
	}
	h.MarkReady()
	if h.Stage() != Draining {
		t.Errorf("MarkReady после Drain: Stage = %v, want draining", h.Stage())
	}
	expect(t, "startup", h.Startup(), 503, "starting\n")
	expect(t, "ready", h.Ready(), 503, "draining\n")

	h2 := New(time.Second)
	h2.MarkReady()
	h2.MarkReady()
	h2.Drain()
	h2.Drain()
	if h2.Stage() != Draining {
		t.Errorf("Stage = %v, want draining", h2.Stage())
	}
	expect(t, "startup после старта и Drain", h2.Startup(), 200, "ok\n")
	if s := Stage(7).String(); s == "starting" || s == "ready" || s == "draining" {
		t.Errorf("Stage(7).String() = %q", s)
	}
}

func TestHiddenNoChecksReady(t *testing.T) {
	h := New(time.Second)
	h.MarkReady()
	expect(t, "ready без проверок", h.Ready(), 200, "ok\n")
}

func TestHiddenConcurrentUse(t *testing.T) {
	h := New(time.Second)
	var wg sync.WaitGroup
	for i := range 20 {
		wg.Go(func() {
			h.AddCheck(fmt.Sprint("dep", i%5), func(context.Context) error { return nil })
			get(t, h.Ready())
			get(t, h.Startup())
			get(t, h.Live())
			h.Stage()
			if i == 10 {
				h.MarkReady()
			}
		})
	}
	wg.Wait()
	h.Drain()
	expect(t, "ready", h.Ready(), 503, "draining\n")
}
