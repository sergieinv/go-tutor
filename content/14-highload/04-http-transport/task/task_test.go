package task

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type item struct {
	SKU string `json:"sku"`
	Qty int    `json:"qty"`
}

// tail — хвост больше 256 КБ: столько Go 1.27 при Close сам не
// дочитывает, поэтому соединение переживёт ответ, только если
// GetJSON дочитает тело сама.
var tail = strings.Repeat(" ", 300<<10)

// countingServer считает принятые сервером TCP-соединения.
func countingServer(t *testing.T, h http.HandlerFunc) (*httptest.Server, *atomic.Int64) {
	t.Helper()
	var conns atomic.Int64
	srv := httptest.NewUnstartedServer(h)
	srv.Config.ReadHeaderTimeout = 5 * time.Second
	srv.Config.ConnState = func(_ net.Conn, s http.ConnState) {
		if s == http.StateNew {
			conns.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)
	return srv, &conns
}

func client(t *testing.T, n int) *http.Client {
	t.Helper()
	c := NewClient(n, 5*time.Second)
	if c == nil {
		t.Fatal("NewClient вернул nil")
	}
	t.Cleanup(c.CloseIdleConnections)
	return c
}

func TestStatusErrorMessage(t *testing.T) {
	if got := (&StatusError{Code: 503}).Error(); got != "unexpected status 503" {
		t.Fatalf("Error() = %q, want %q", got, "unexpected status 503")
	}
}

func TestGetJSONDecodes(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"sku":"A-1","qty":3}`)
	})
	var got item
	if err := GetJSON(context.Background(), client(t, 2), srv.URL, &got); err != nil {
		t.Fatalf("GetJSON: %v", err)
	}
	if got != (item{"A-1", 3}) {
		t.Fatalf("got %+v", got)
	}
}

func TestGetJSONStatusError(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "нет такого товара", http.StatusNotFound)
	})
	var got item
	err := GetJSON(context.Background(), client(t, 2), srv.URL, &got)
	var se *StatusError
	if !errors.As(err, &se) || se.Code != http.StatusNotFound {
		t.Fatalf("err = %v, want *StatusError{Code: 404}", err)
	}
}

func TestReuseAfterErrorStatus(t *testing.T) {
	srv, conns := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		io.WriteString(w, "<html>большая страница ошибки от прокси"+tail)
	})
	c := client(t, 2)
	for i := range 20 {
		var got item
		var se *StatusError
		if err := GetJSON(context.Background(), c, srv.URL, &got); !errors.As(err, &se) {
			t.Fatalf("запрос %d: err = %v, want *StatusError", i, err)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Fatalf("20 запросов с ответом 500 открыли %d соединений, want 1 — тело не дочитано?", n)
	}
}

func TestReuseAfterTrailingData(t *testing.T) {
	srv, conns := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"sku":"B-2","qty":7}`+tail)
	})
	c := client(t, 2)
	for i := range 20 {
		var got item
		if err := GetJSON(context.Background(), c, srv.URL, &got); err != nil {
			t.Fatalf("запрос %d: %v", i, err)
		}
		if got != (item{"B-2", 7}) {
			t.Fatalf("запрос %d: got %+v", i, got)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Fatalf("20 запросов открыли %d соединений, want 1 — хвост после JSON не дочитан?", n)
	}
}

func TestNewClientTransport(t *testing.T) {
	c := NewClient(8, 3*time.Second)
	tr, ok := c.Transport.(*http.Transport)
	if !ok || tr == nil {
		t.Fatalf("Transport = %T, want *http.Transport", c.Transport)
	}
	if tr.MaxConnsPerHost != 8 || tr.MaxIdleConnsPerHost != 8 {
		t.Fatalf("MaxConnsPerHost = %d, MaxIdleConnsPerHost = %d, want 8 и 8", tr.MaxConnsPerHost, tr.MaxIdleConnsPerHost)
	}
	if c.Timeout != 3*time.Second {
		t.Fatalf("Timeout = %v, want 3s", c.Timeout)
	}
}
