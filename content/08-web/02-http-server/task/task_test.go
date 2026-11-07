package task

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func do(h http.Handler, method, target string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
	return rec
}

func TestCounterFlow(t *testing.T) {
	c := &Counter{}
	steps := []struct {
		method, target string
		code           int
		body           string
	}{
		{"GET", "/", 200, "0\n"},
		{"POST", "/", 200, "1\n"},
		{"POST", "/?by=5", 200, "6\n"},
		{"GET", "/", 200, "6\n"},
		{"DELETE", "/", 204, ""},
		{"GET", "/", 200, "0\n"},
	}
	for _, s := range steps {
		rec := do(c, s.method, s.target)
		if rec.Code != s.code || rec.Body.String() != s.body {
			t.Fatalf("%s %s = %d %q; want %d %q", s.method, s.target, rec.Code, rec.Body.String(), s.code, s.body)
		}
	}
}

func TestCounterHeaders(t *testing.T) {
	rec := do(&Counter{}, "GET", "/")
	h := rec.Result().Header
	if h.Get("Content-Type") != "text/plain; charset=utf-8" || h.Get("Cache-Control") != "no-store" {
		t.Errorf("заголовки GET: %v", h)
	}
}

func TestCounterMethodNotAllowed(t *testing.T) {
	rec := do(&Counter{}, "PUT", "/")
	if rec.Code != 405 || rec.Result().Header.Get("Allow") != "GET, HEAD, POST, DELETE" {
		t.Errorf("PUT = %d, Allow %q; want 405, \"GET, HEAD, POST, DELETE\"", rec.Code, rec.Result().Header.Get("Allow"))
	}
}
