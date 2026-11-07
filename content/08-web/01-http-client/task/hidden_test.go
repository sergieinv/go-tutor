package task

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func jsonServer(t *testing.T, contentType, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestRequestShape(t *testing.T) {
	var method, accept atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method.Store(r.Method)
		accept.Store(r.Header.Get("Accept"))
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{}`)
	}))
	defer srv.Close()

	var v map[string]any
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &v); err != nil {
		t.Fatalf("GetJSON = %v", err)
	}
	if method.Load() != "GET" || accept.Load() != "application/json" {
		t.Errorf("метод %v, Accept %q; want GET, application/json", method.Load(), accept.Load())
	}
}

func TestNilClient(t *testing.T) {
	srv := jsonServer(t, "application/json", `[1,2,3]`)
	var v []int
	if err := GetJSON(context.Background(), nil, srv.URL, 100, &v); err != nil || len(v) != 3 {
		t.Fatalf("GetJSON(nil client) = %v, v = %v", err, v)
	}
}

func TestContentTypeVariants(t *testing.T) {
	ok := []string{"application/json", "application/json; charset=utf-8", "Application/JSON"}
	for _, ct := range ok {
		srv := jsonServer(t, ct, `{"id":1}`)
		var u user
		if err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &u); err != nil || u.ID != 1 {
			t.Errorf("Content-Type %q: GetJSON = %v, u = %+v; want nil", ct, err, u)
		}
	}
	bad := []string{"", "text/plain", "application/jsonp", "application/problem+json", ";;;"}
	for _, ct := range bad {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header()["Content-Type"] = []string{ct} // пустое значение отключит автоопределение типа
			fmt.Fprint(w, `{"id":1}`)
		}))
		u := user{Name: "старое"}
		err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &u)
		srv.Close()
		if !errors.Is(err, ErrNotJSON) || u != (user{Name: "старое"}) {
			t.Errorf("Content-Type %q: GetJSON = %v, u = %+v; want ErrNotJSON и неизменный v", ct, err, u)
		}
	}
}

func TestStatusCodes(t *testing.T) {
	for _, code := range []int{201, 299} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			fmt.Fprint(w, `{"id":2}`)
		}))
		var u user
		err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &u)
		srv.Close()
		if err != nil || u.ID != 2 {
			t.Errorf("статус %d: GetJSON = %v; want nil", code, err)
		}
	}
	for _, code := range []int{300, 400, 500, 503} {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json") // статус проверяется раньше типа
			w.WriteHeader(code)
			fmt.Fprint(w, `{"id":2}`)
		}))
		u := user{ID: 9}
		err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &u)
		srv.Close()
		var se *StatusError
		if !errors.As(err, &se) || se.StatusCode != code || u.ID != 9 {
			t.Errorf("статус %d: GetJSON = %v, u = %+v; want *StatusError{%d} и неизменный v", code, err, u, code)
		}
	}
	// 404 с text/plain — ошибка статуса, а не ErrNotJSON.
	srv := httptest.NewServer(http.NotFoundHandler())
	defer srv.Close()
	var se *StatusError
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &user{}); !errors.As(err, &se) || errors.Is(err, ErrNotJSON) {
		t.Errorf("404 text/plain: GetJSON = %v; want только *StatusError", err)
	}
}

func TestSizeLimit(t *testing.T) {
	body := `{"name":"` + strings.Repeat("я", 20) + `"}` // 52 байта
	srv := jsonServer(t, "application/json", body)

	var u user
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, int64(len(body)), &u); err != nil {
		t.Errorf("тело ровно maxBytes: GetJSON = %v; want nil", err)
	}
	u = user{Name: "старое"}
	err := GetJSON(context.Background(), srv.Client(), srv.URL, int64(len(body)-1), &u)
	if !errors.Is(err, ErrTooLarge) {
		t.Errorf("тело на 1 байт больше maxBytes: GetJSON = %v; want ErrTooLarge", err)
	}
	if u.Name != "старое" {
		t.Errorf("при ErrTooLarge v изменён: %+v", u)
	}

	// Огромный ответ: функция не должна читать его целиком.
	huge := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		chunk := []byte(strings.Repeat(" ", 64<<10))
		for range 4096 { // 256 МБ, если читать до конца
			if _, err := w.Write(chunk); err != nil {
				return
			}
		}
	}))
	defer huge.Close()
	if err := GetJSON(context.Background(), huge.Client(), huge.URL, 1<<20, &u); !errors.Is(err, ErrTooLarge) {
		t.Errorf("огромный ответ: GetJSON = %v; want ErrTooLarge", err)
	}
}

func TestBadJSON(t *testing.T) {
	cases := []struct {
		body string
		as   any
	}{
		{`{"id":`, new(*json.SyntaxError)},
		{`{"id":1} {"id":2}`, new(*json.SyntaxError)},
		{`{"id":1}x`, new(*json.SyntaxError)},
		{``, new(*json.SyntaxError)},
		{`{"id":"семь"}`, new(*json.UnmarshalTypeError)},
	}
	for _, c := range cases {
		srv := jsonServer(t, "application/json", c.body)
		var u user
		err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &u)
		if err == nil || !errors.As(err, c.as) {
			t.Errorf("тело %q: GetJSON = %v; want обёрнутую ошибку %T", c.body, err, c.as)
		}
		if errors.Is(err, ErrNotJSON) || errors.Is(err, ErrTooLarge) {
			t.Errorf("тело %q: GetJSON = %v; ошибка разбора не должна быть ErrNotJSON/ErrTooLarge", c.body, err)
		}
	}
	srv := jsonServer(t, "application/json", " \n{\"id\":3}\n\t")
	var u user
	if err := GetJSON(context.Background(), srv.Client(), srv.URL, 100, &u); err != nil || u.ID != 3 {
		t.Errorf("JSON с пробелами вокруг: GetJSON = %v; want nil", err)
	}
}

func TestContextErrors(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-release:
		}
	}))
	defer srv.Close()
	defer close(release)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	err := GetJSON(ctx, srv.Client(), srv.URL, 100, &user{})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("дедлайн контекста: GetJSON = %v; want errors.Is(err, context.DeadlineExceeded)", err)
	}

	ctx, cancel = context.WithCancel(context.Background())
	cancel()
	err = GetJSON(ctx, srv.Client(), srv.URL, 100, &user{})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("отменённый контекст: GetJSON = %v; want errors.Is(err, context.Canceled)", err)
	}

	if err := GetJSON(context.Background(), srv.Client(), "://bad url", 100, &user{}); err == nil {
		t.Error("некорректный URL: GetJSON = nil; want ошибку")
	}
}

// Тела, уложившиеся в лимит, дочитываются и закрываются — тогда все
// запросы идут по одному keep-alive соединению. Тело ошибки больше
// 256 КБ: такой остаток транспорт не дочитывает сам.
func TestConnectionReuse(t *testing.T) {
	var conns atomic.Int32
	bigErr := strings.Repeat("e", 300<<10)
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/ok":
			w.Header().Set("Content-Type", "application/json")
			fmt.Fprint(w, `{"id":1}   `)
		case "/html":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, bigErr)
		default:
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusInternalServerError)
			fmt.Fprint(w, bigErr)
		}
	}))
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	defer srv.Close()

	client := srv.Client()
	for range 3 {
		for _, p := range []string{"/ok", "/fail", "/html"} {
			GetJSON(context.Background(), client, srv.URL+p, 1<<20, &user{})
		}
	}
	if n := conns.Load(); n != 1 {
		t.Errorf("9 последовательных запросов открыли %d соединений; want 1: тело нужно дочитать и закрыть", n)
	}
}
