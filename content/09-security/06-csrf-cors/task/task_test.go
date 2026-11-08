package task

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
)

const app = "https://app.example"

// api — хендлер-счётчик: сколько раз до него дошли запросы.
type api struct{ calls atomic.Int32 }

func (a *api) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	a.calls.Add(1)
	w.Write([]byte("ok"))
}

func corsMW(t *testing.T, origins ...string) (http.Handler, *api) {
	t.Helper()
	mw, err := NewCORS(origins)
	if err != nil {
		t.Fatalf("NewCORS(%q): %v", origins, err)
	}
	if mw == nil {
		t.Fatalf("NewCORS вернул nil без ошибки")
	}
	next := &api{}
	return mw(next), next
}

func protected(t *testing.T, origins ...string) (http.Handler, *api) {
	t.Helper()
	next := &api{}
	h, err := Protect(origins, next)
	if err != nil {
		t.Fatalf("Protect(%q): %v", origins, err)
	}
	if h == nil {
		t.Fatalf("Protect вернул nil без ошибки")
	}
	return h, next
}

// request собирает запрос к https://api.example; headers — пары «имя, значение».
func request(method, path string, headers ...string) *http.Request {
	r := httptest.NewRequest(method, "https://api.example"+path, strings.NewReader("amount=100"))
	r.AddCookie(&http.Cookie{Name: "session", Value: "victim"})
	for i := 0; i+1 < len(headers); i += 2 {
		r.Header.Set(headers[i], headers[i+1])
	}
	return r
}

func serve(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)
	return rec
}

func TestCORSAllowed(t *testing.T) {
	h, next := corsMW(t, app, "http://localhost:3000")
	for _, o := range []string{app, "http://localhost:3000"} {
		rec := serve(h, request("GET", "/me", "Origin", o))
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != o {
			t.Errorf("Origin %s: Access-Control-Allow-Origin = %q, want %q", o, got, o)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Origin %s: Access-Control-Allow-Credentials = %q, want true", o, got)
		}
		if !slices.Contains(rec.Header().Values("Vary"), "Origin") {
			t.Errorf("Origin %s: нет Vary: Origin", o)
		}
	}
	if next.calls.Load() != 2 {
		t.Errorf("next вызван %d раз, want 2", next.calls.Load())
	}
}

func TestCORSOtherOrigin(t *testing.T) {
	h, next := corsMW(t, app)
	rec := serve(h, request("GET", "/me", "Origin", "https://other.example"))
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Errorf("чужой origin: Access-Control-Allow-Origin = %q, want пусто", got)
	}
	// CORS не блокирует запрос — он лишь не разрешает читать ответ.
	if next.calls.Load() != 1 || rec.Code != 200 {
		t.Errorf("запрос с чужим origin должен дойти до next: calls=%d code=%d", next.calls.Load(), rec.Code)
	}
}

func TestPreflight(t *testing.T) {
	h, next := corsMW(t, app)
	rec := serve(h, request("OPTIONS", "/orders", "Origin", app, "Access-Control-Request-Method", "PUT"))
	if rec.Code != http.StatusNoContent {
		t.Fatalf("preflight: код %d, want 204", rec.Code)
	}
	want := map[string]string{
		"Access-Control-Allow-Origin":      app,
		"Access-Control-Allow-Credentials": "true",
		"Access-Control-Allow-Methods":     "GET, POST, PUT, DELETE",
		"Access-Control-Allow-Headers":     "Content-Type",
		"Access-Control-Max-Age":           "600",
	}
	for k, v := range want {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
	if next.calls.Load() != 0 {
		t.Errorf("preflight дошёл до next")
	}
}

func TestNewCORSRejectsBadOrigin(t *testing.T) {
	if _, err := NewCORS([]string{app, "*"}); !errors.Is(err, ErrBadOrigin) {
		t.Errorf("NewCORS с \"*\": %v, want ErrBadOrigin", err)
	}
}

func TestProtect(t *testing.T) {
	h, next := protected(t, app)
	rec := serve(h, request("POST", "/transfer", "Sec-Fetch-Site", "same-origin", "Origin", "https://api.example"))
	if rec.Code != 200 || next.calls.Load() != 1 {
		t.Fatalf("POST того же origin: код %d, calls=%d", rec.Code, next.calls.Load())
	}
	rec = serve(h, request("POST", "/transfer", "Sec-Fetch-Site", "cross-site", "Origin", app))
	if rec.Code != 200 || next.calls.Load() != 2 {
		t.Fatalf("POST с разрешённого origin: код %d, calls=%d", rec.Code, next.calls.Load())
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != app {
		t.Errorf("POST с разрешённого origin: Access-Control-Allow-Origin = %q", got)
	}
}
