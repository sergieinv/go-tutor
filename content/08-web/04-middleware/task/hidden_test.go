package task

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestChainEmpty(t *testing.T) {
	called := false
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })
	Chain(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if !called {
		t.Error("Chain(h) без middleware не вызвал h")
	}
	// Одна middleware, подменяющая ответ, — должна сработать.
	block := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusTeapot) })
	}
	rec := httptest.NewRecorder()
	Chain(h, block).ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != http.StatusTeapot {
		t.Errorf("Chain(h, block) = %d; want 418", rec.Code)
	}
}

func logOne(t *testing.T, h http.HandlerFunc) Entry {
	t.Helper()
	var got []Entry
	Logging(func(e Entry) { got = append(got, e) })(h).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("PUT", "/a/b?q=1", nil))
	if len(got) != 1 {
		t.Fatalf("record вызван %d раз; want 1", len(got))
	}
	return got[0]
}

func TestLoggingCases(t *testing.T) {
	cases := []struct {
		name string
		h    http.HandlerFunc
		want Entry
	}{
		{"ничего не записано", func(w http.ResponseWriter, r *http.Request) {}, Entry{"PUT", "/a/b", 200, 0}},
		{"только Write", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "abc")
			io.WriteString(w, "де") // 4 байта
		}, Entry{"PUT", "/a/b", 200, 7}},
		{"повторный WriteHeader", func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			w.WriteHeader(http.StatusInternalServerError)
		}, Entry{"PUT", "/a/b", 404, 0}},
		{"Write, потом WriteHeader", func(w http.ResponseWriter, r *http.Request) {
			io.WriteString(w, "x")
			w.WriteHeader(http.StatusInternalServerError)
		}, Entry{"PUT", "/a/b", 200, 1}},
		{"http.Error", func(w http.ResponseWriter, r *http.Request) {
			http.Error(w, "nope", http.StatusForbidden)
		}, Entry{"PUT", "/a/b", 403, 5}},
		{"ReadFrom через io.Copy", func(w http.ResponseWriter, r *http.Request) {
			io.Copy(w, strings.NewReader("12345678"))
		}, Entry{"PUT", "/a/b", 200, 8}},
	}
	for _, c := range cases {
		if got := logOne(t, c.h); got != c.want {
			t.Errorf("%s: %+v; want %+v", c.name, got, c.want)
		}
	}
}

func TestLoggingAfterHandler(t *testing.T) {
	var events []string
	h := Logging(func(e Entry) { events = append(events, "record") })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		events = append(events, "handler")
	}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	if strings.Join(events, ",") != "handler,record" {
		t.Errorf("порядок: %v; want handler, record", events)
	}
}

func TestLoggingPassesResponseThrough(t *testing.T) {
	h := Logging(func(Entry) {})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-A", "1")
		w.WriteHeader(http.StatusAccepted)
		io.WriteString(w, "body")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if rec.Code != 202 || rec.Body.String() != "body" || rec.Header().Get("X-A") != "1" {
		t.Errorf("ответ через обёртку: %d %q %v; want 202 body X-A=1", rec.Code, rec.Body.String(), rec.Header())
	}
}

func TestLoggingUnwrap(t *testing.T) {
	var flushErr error
	h := Logging(func(Entry) {})(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "часть")
		flushErr = http.NewResponseController(w).Flush()
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if flushErr != nil || !rec.Flushed {
		t.Errorf("Flush через обёртку: err=%v, Flushed=%v; нужен метод Unwrap() http.ResponseWriter", flushErr, rec.Flushed)
	}
}

func TestRequestIDHeader(t *testing.T) {
	calls := 0
	gen := func() string { calls++; return "generated" }
	cases := []struct {
		in, want string
		genCall  bool
	}{
		{"abc-DEF_123", "abc-DEF_123", false},
		{strings.Repeat("a", 64), strings.Repeat("a", 64), false},
		{strings.Repeat("a", 65), "generated", true},
		{"", "generated", true},
		{"has space", "generated", true},
		{"id;drop", "generated", true},
		{"ид", "generated", true},
		{"a.b", "generated", true},
	}
	for _, c := range cases {
		calls = 0
		var seen string
		h := RequestID(gen)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			seen = RequestIDFrom(r.Context())
		}))
		req := httptest.NewRequest("GET", "/", nil)
		if c.in != "" {
			req.Header.Set("X-Request-ID", c.in)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if seen != c.want || rec.Header().Get("X-Request-ID") != c.want {
			t.Errorf("входящий id %q: в контексте %q, в ответе %q; want %q", c.in, seen, rec.Header().Get("X-Request-ID"), c.want)
		}
		if (calls == 1) != c.genCall || calls > 1 {
			t.Errorf("входящий id %q: gen вызван %d раз", c.in, calls)
		}
	}
}

func TestRequestIDBeforeNext(t *testing.T) {
	// Хендлер сразу пишет статус: заголовок должен быть задан раньше.
	h := RequestID(func() string { return "early" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	if got := rec.Result().Header.Get("X-Request-ID"); got != "early" {
		t.Errorf("X-Request-ID в отправленных заголовках = %q; want early", got)
	}
}

type otherKey struct{}

func TestRequestIDContext(t *testing.T) {
	if got := RequestIDFrom(context.Background()); got != "" {
		t.Errorf("RequestIDFrom(Background) = %q; want \"\"", got)
	}
	if got := RequestIDFrom(context.WithValue(context.Background(), "requestID", "x")); got != "" {
		t.Errorf("RequestIDFrom по чужому ключу = %q; want \"\"", got)
	}

	parent := context.WithValue(context.Background(), otherKey{}, "сохранено")
	req := httptest.NewRequest("GET", "/", nil).WithContext(parent)
	var inner *http.Request
	h := RequestID(func() string { return "r1" })(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		inner = r
	}))
	h.ServeHTTP(httptest.NewRecorder(), req)
	if inner == nil || inner.Context().Value(otherKey{}) != "сохранено" {
		t.Fatal("контекст next должен быть производным от контекста запроса")
	}
	if RequestIDFrom(req.Context()) != "" {
		t.Error("исходный запрос изменён: его контекст содержит id")
	}
}

func TestFullChain(t *testing.T) {
	var entries []Entry
	n := 0
	gen := func() string { n++; return "id" + string(rune('0'+n)) }
	app := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, RequestIDFrom(r.Context()))
	})
	h := Chain(app, Logging(func(e Entry) { entries = append(entries, e) }), RequestID(gen))
	for range 2 {
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/x", nil))
	}
	want := []Entry{{"GET", "/x", 200, 3}, {"GET", "/x", 200, 3}}
	if len(entries) != 2 || entries[0] != want[0] || entries[1] != want[1] {
		t.Errorf("журнал полной цепочки: %+v; want %+v", entries, want)
	}
}
