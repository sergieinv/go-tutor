package task

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChainOrder(t *testing.T) {
	var trace []string
	mw := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				trace = append(trace, name+">")
				next.ServeHTTP(w, r)
				trace = append(trace, "<"+name)
			})
		}
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { trace = append(trace, "h") })
	Chain(h, mw("a"), mw("b"), mw("c")).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if got := strings.Join(trace, " "); got != "a> b> c> h <c <b <a" {
		t.Errorf("порядок вызовов: %s; want a> b> c> h <c <b <a", got)
	}
}

func TestLoggingStatus(t *testing.T) {
	var got []Entry
	h := Logging(func(e Entry) { got = append(got, e) })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		fmt.Fprint(w, "hello")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("POST", "/items?x=1", nil))
	want := Entry{Method: "POST", Path: "/items", Status: 201, Bytes: 5}
	if len(got) != 1 || got[0] != want {
		t.Errorf("записи журнала: %+v; want [%+v]", got, want)
	}
}

func TestRequestIDGenerated(t *testing.T) {
	var seen string
	h := RequestID(func() string { return "gen-1" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = RequestIDFrom(r.Context())
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if seen != "gen-1" || rec.Header().Get("X-Request-ID") != "gen-1" {
		t.Errorf("id в хендлере %q, в ответе %q; want gen-1", seen, rec.Header().Get("X-Request-ID"))
	}
}
