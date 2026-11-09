package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// get вызывает обработчик пробы и возвращает код и тело.
func get(t *testing.T, h http.Handler) (int, string) {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	return rec.Code, rec.Body.String()
}

func expect(t *testing.T, name string, h http.Handler, code int, body string) {
	t.Helper()
	gotCode, gotBody := get(t, h)
	if gotCode != code || gotBody != body {
		t.Errorf("%s = %d %q, want %d %q", name, gotCode, gotBody, code, body)
	}
}

func TestStageString(t *testing.T) {
	for s, want := range map[Stage]string{Starting: "starting", Ready: "ready", Draining: "draining"} {
		if got := s.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int(s), got, want)
		}
	}
}

func TestLifecycle(t *testing.T) {
	h := New(time.Second)
	if h.Stage() != Starting {
		t.Fatalf("новый Health: Stage = %v, want starting", h.Stage())
	}
	expect(t, "startup", h.Startup(), 503, "starting\n")
	expect(t, "ready", h.Ready(), 503, "starting\n")
	expect(t, "live", h.Live(), 200, "ok\n")

	h.MarkReady()
	if h.Stage() != Ready {
		t.Fatalf("после MarkReady: Stage = %v, want ready", h.Stage())
	}
	expect(t, "startup", h.Startup(), 200, "ok\n")
	expect(t, "ready", h.Ready(), 200, "ok\n")

	h.Drain()
	if h.Stage() != Draining {
		t.Fatalf("после Drain: Stage = %v, want draining", h.Stage())
	}
	expect(t, "ready", h.Ready(), 503, "draining\n")
	expect(t, "startup", h.Startup(), 200, "ok\n")
	expect(t, "live", h.Live(), 200, "ok\n")
}

func TestReadyChecks(t *testing.T) {
	h := New(time.Second)
	dbErr := errors.New("dial tcp 10.0.0.5:5432: connection refused")
	h.AddCheck("db", func(context.Context) error { return dbErr })
	h.AddCheck("cache", func(context.Context) error { return nil })
	h.MarkReady()
	expect(t, "ready", h.Ready(), 503, "not ready: db\n")
	expect(t, "live", h.Live(), 200, "ok\n")
	dbErr = nil
	expect(t, "ready", h.Ready(), 200, "ok\n")
}

func TestReadyTwoFailedChecks(t *testing.T) {
	h := New(time.Second)
	down := errors.New("down")
	h.AddCheck("kafka", func(context.Context) error { return down })
	h.AddCheck("db", func(context.Context) error { return down })
	h.MarkReady()
	// По алфавиту, через запятую без пробелов.
	expect(t, "ready", h.Ready(), 503, "not ready: db,kafka\n")
}
