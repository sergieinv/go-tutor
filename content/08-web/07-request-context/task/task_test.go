package task

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// wait отвечает value через d или ошибкой контекста при отмене.
func wait(d time.Duration, value string) Fetcher {
	return func(ctx context.Context, id string) (string, error) {
		select {
		case <-time.After(d):
			return value + ":" + id, nil
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
}

func serve(t *testing.T, h http.Handler, target string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", target, nil))
	return rec
}

func check(t *testing.T, rec *httptest.ResponseRecorder, code int, body string) {
	t.Helper()
	if rec.Code != code || rec.Body.String() != body {
		t.Fatalf("ответ %d %q, want %d %q", rec.Code, rec.Body.String(), code, body)
	}
}

func TestOK(t *testing.T) {
	h := Handler(wait(5*time.Millisecond, "item"), time.Second)
	check(t, serve(t, h, "/?id=7"), 200, "item:7")
}

func TestMissingID(t *testing.T) {
	h := Handler(wait(0, "item"), time.Second)
	check(t, serve(t, h, "/"), 400, "missing id\n")
}

func TestNotFound(t *testing.T) {
	h := Handler(func(context.Context, string) (string, error) { return "", ErrNotFound }, time.Second)
	check(t, serve(t, h, "/?id=1"), 404, "not found\n")
}

func TestTimeout(t *testing.T) {
	h := Handler(wait(time.Hour, "never"), 30*time.Millisecond)
	check(t, serve(t, h, "/?id=1"), 504, "timeout\n")
}

func TestOtherError(t *testing.T) {
	h := Handler(func(context.Context, string) (string, error) { return "", errors.New("db down") }, time.Second)
	check(t, serve(t, h, "/?id=1"), 500, "internal error\n")
}
