package task

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type resp struct {
	code   int
	header http.Header
	body   string
}

func call(h http.Handler, method, target, body string) resp {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, rd))
	res := rec.Result()
	return resp{res.StatusCode, res.Header, rec.Body.String()}
}

func TestIndex(t *testing.T) {
	if r := call(NewRouter(), "GET", "/", ""); r.code != 200 || r.body != "notes api\n" {
		t.Errorf("GET / = %d %q; want 200 \"notes api\\n\"", r.code, r.body)
	}
}

func TestCreateAndGet(t *testing.T) {
	h := NewRouter()
	r := call(h, "POST", "/notes/", "купить молоко")
	if r.code != 201 || r.header.Get("Location") != "/notes/1" || r.body != "1\n" {
		t.Fatalf("POST /notes/ = %d Location=%q %q; want 201 /notes/1 \"1\\n\"", r.code, r.header.Get("Location"), r.body)
	}
	if r := call(h, "GET", "/notes/1", ""); r.code != 200 || r.body != "купить молоко\n" {
		t.Errorf("GET /notes/1 = %d %q", r.code, r.body)
	}
}

func TestNotFoundAndMethodNotAllowed(t *testing.T) {
	h := NewRouter()
	if r := call(h, "GET", "/notes/99", ""); r.code != 404 {
		t.Errorf("GET /notes/99 = %d; want 404", r.code)
	}
	r := call(h, "PUT", "/notes/1", "")
	if r.code != 405 || r.header.Get("Allow") != "DELETE, GET, HEAD" {
		t.Errorf("PUT /notes/1 = %d Allow=%q; want 405 \"DELETE, GET, HEAD\"", r.code, r.header.Get("Allow"))
	}
}
