package task

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

// records разбирает JSON-строки лога.
func records(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("строка лога не JSON: %q", line)
		}
		out = append(out, m)
	}
	return out
}

func serve(h http.Handler, method, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, path, nil))
	return rec
}

func errorBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]string {
	t.Helper()
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Errorf("Content-Type %q, want application/json", ct)
	}
	var m map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("тело ответа не JSON-объект строк: %q", rec.Body.String())
	}
	return m
}

var idRe = regexp.MustCompile(`^[A-Z2-7]{26}$`)

func TestSecret(t *testing.T) {
	s := Secret("hunter2")
	if s.Reveal() != "hunter2" {
		t.Errorf("Reveal() = %q", s.Reveal())
	}
	if got := fmt.Sprint(s); got != "[REDACTED]" {
		t.Errorf("fmt.Sprint = %q, want [REDACTED]", got)
	}
}

func TestHandleOK(t *testing.T) {
	var buf bytes.Buffer
	h := Handle(NewLogger(&buf), func(w http.ResponseWriter, r *http.Request) error {
		fmt.Fprint(w, "ok")
		return nil
	})
	a, b := serve(h, "GET", "/"), serve(h, "GET", "/")
	if a.Code != 200 || a.Body.String() != "ok" {
		t.Fatalf("ответ %d %q, want 200 \"ok\"", a.Code, a.Body.String())
	}
	idA, idB := a.Header().Get("X-Request-ID"), b.Header().Get("X-Request-ID")
	if !idRe.MatchString(idA) || !idRe.MatchString(idB) || idA == idB {
		t.Fatalf("X-Request-ID %q и %q: want разные строки crypto/rand.Text()", idA, idB)
	}
}

func TestHandleInternalError(t *testing.T) {
	var buf bytes.Buffer
	h := Handle(NewLogger(&buf), func(w http.ResponseWriter, r *http.Request) error {
		return errors.New("disk full")
	})
	rec := serve(h, "POST", "/upload")
	if rec.Code != 500 {
		t.Fatalf("код %d, want 500", rec.Code)
	}
	id := rec.Header().Get("X-Request-ID")
	body := errorBody(t, rec)
	if len(body) != 2 || body["error"] != "internal error" || body["request_id"] != id || id == "" {
		t.Fatalf("тело %v, want error=internal error и request_id=%q", body, id)
	}
	logs := records(t, &buf)
	if len(logs) != 1 {
		t.Fatalf("записей в логе %d, want 1:\n%s", len(logs), buf.String())
	}
	want := map[string]any{"level": "ERROR", "msg": "request failed", "request_id": id,
		"method": "POST", "path": "/upload", "err": "disk full"}
	for k, v := range want {
		if logs[0][k] != v {
			t.Errorf("в логе %s = %v, want %v", k, logs[0][k], v)
		}
	}
}

func TestHandlePublicError(t *testing.T) {
	var buf bytes.Buffer
	h := Handle(NewLogger(&buf), func(w http.ResponseWriter, r *http.Request) error {
		return fmt.Errorf("get order: %w", &PublicError{Status: 404, Msg: "order not found"})
	})
	rec := serve(h, "GET", "/orders/7")
	body := errorBody(t, rec)
	if rec.Code != 404 || body["error"] != "order not found" || body["request_id"] != rec.Header().Get("X-Request-ID") {
		t.Fatalf("ответ %d %v, want 404 и error=order not found", rec.Code, body)
	}
}

func TestLoggerKeepsOrdinaryKeys(t *testing.T) {
	var buf bytes.Buffer
	NewLogger(&buf).Info("login", "user", "bob", "attempt", 2)
	logs := records(t, &buf)
	if len(logs) != 1 || logs[0]["user"] != "bob" || logs[0]["attempt"] != 2.0 || logs[0]["msg"] != "login" || logs[0]["level"] != "INFO" {
		t.Fatalf("лог %s: обычные ключи должны остаться как есть", buf.String())
	}
}
