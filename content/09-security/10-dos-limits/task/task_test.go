package task

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testLimits() Limits {
	return Limits{
		HeaderTimeout:   time.Second,
		ReadTimeout:     5 * time.Second,
		WriteTimeout:    5 * time.Second,
		IdleTimeout:     30 * time.Second,
		MaxHeaderBytes:  8 << 10,
		MaxBodyBytes:    1 << 10,
		MaxDecompressed: 4 << 10,
		MaxInFlight:     2,
	}
}

// echo читает тело целиком: превышение лимита — 413, другая ошибка — 400,
// иначе 200 и тело. Что видел хендлер — в полях.
type echo struct {
	calls    atomic.Int32
	encoding atomic.Value // Content-Encoding, который увидел хендлер
	length   atomic.Int64 // r.ContentLength
}

func (e *echo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	e.calls.Add(1)
	e.encoding.Store(r.Header.Get("Content-Encoding"))
	e.length.Store(r.ContentLength)
	data, err := io.ReadAll(r.Body)
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &tooBig):
		http.Error(w, fmt.Sprintf("too large: read %d", len(data)), http.StatusRequestEntityTooLarge)
	case err != nil:
		http.Error(w, "read error", http.StatusBadRequest)
	default:
		w.Write(data)
	}
}

func gz(t *testing.T, data []byte) []byte {
	t.Helper()
	var b bytes.Buffer
	zw := gzip.NewWriter(&b)
	if _, err := zw.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func post(h http.Handler, body []byte, encoding string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/", bytes.NewReader(body))
	if encoding != "" {
		req.Header.Set("Content-Encoding", encoding)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestPlainBody(t *testing.T) {
	e := &echo{}
	rec := post(Protect(e, testLimits()), []byte("hello"), "")
	if rec.Code != 200 || rec.Body.String() != "hello" {
		t.Fatalf("got %d %q, want 200 \"hello\"", rec.Code, rec.Body.String())
	}
}

func TestGzipBody(t *testing.T) {
	e := &echo{}
	rec := post(Protect(e, testLimits()), gz(t, []byte("hello, gzip")), "gzip")
	if rec.Code != 200 || rec.Body.String() != "hello, gzip" {
		t.Fatalf("got %d %q, want 200 \"hello, gzip\" (распакованное тело)", rec.Code, rec.Body.String())
	}
	if enc := e.encoding.Load(); enc != "" {
		t.Errorf("хендлер видит Content-Encoding %q, want удалённый заголовок", enc)
	}
	if n := e.length.Load(); n != -1 {
		t.Errorf("хендлер видит ContentLength %d, want -1", n)
	}
}

func TestRejectedBeforeHandler(t *testing.T) {
	tests := []struct {
		name     string
		body     []byte
		encoding string
		want     int
	}{
		{"unsupported encoding", []byte("x"), "br", http.StatusUnsupportedMediaType},
		{"not gzip", []byte("definitely not gzip"), "gzip", http.StatusBadRequest},
	}
	for _, tt := range tests {
		e := &echo{}
		rec := post(Protect(e, testLimits()), tt.body, tt.encoding)
		if rec.Code != tt.want {
			t.Errorf("%s: код %d, want %d", tt.name, rec.Code, tt.want)
		}
		if n := e.calls.Load(); n != 0 {
			t.Errorf("%s: next вызван %d раз, want 0", tt.name, n)
		}
	}
}

func TestNewServerFields(t *testing.T) {
	l := testLimits()
	srv := NewServer(&echo{}, l)
	if srv.ReadHeaderTimeout != l.HeaderTimeout || srv.ReadTimeout != l.ReadTimeout ||
		srv.WriteTimeout != l.WriteTimeout || srv.IdleTimeout != l.IdleTimeout ||
		srv.MaxHeaderBytes != l.MaxHeaderBytes {
		t.Fatalf("сервер %+v не соответствует лимитам %+v", srv, l)
	}
	if srv.Handler == nil {
		t.Fatal("Handler == nil")
	}
	rec := httptest.NewRecorder()
	srv.Handler.ServeHTTP(rec, httptest.NewRequest("POST", "/", strings.NewReader("ok")))
	if rec.Code != 200 || rec.Body.String() != "ok" {
		t.Fatalf("Handler сервера: %d %q, want 200 \"ok\"", rec.Code, rec.Body.String())
	}
}
