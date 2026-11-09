package task

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNewClientDefaults(t *testing.T) {
	a, b := NewClient(0, 0), NewClient(-5, -time.Second)
	for _, c := range []*http.Client{a, b} {
		tr, ok := c.Transport.(*http.Transport)
		if !ok || tr == nil {
			t.Fatalf("Transport = %T, want *http.Transport", c.Transport)
		}
		if tr == http.DefaultTransport {
			t.Fatal("NewClient использует http.DefaultTransport, а нужен свой транспорт")
		}
		if tr.MaxConnsPerHost != 1 || tr.MaxIdleConnsPerHost != 1 {
			t.Fatalf("при n < 1: MaxConnsPerHost = %d, MaxIdleConnsPerHost = %d, want 1 и 1", tr.MaxConnsPerHost, tr.MaxIdleConnsPerHost)
		}
		if tr.IdleConnTimeout <= 0 || tr.TLSHandshakeTimeout <= 0 {
			t.Fatalf("IdleConnTimeout = %v, TLSHandshakeTimeout = %v, want > 0", tr.IdleConnTimeout, tr.TLSHandshakeTimeout)
		}
		if tr.DialContext == nil {
			t.Fatal("DialContext не задан — нет таймаута на установку соединения")
		}
		if c.Timeout != 30*time.Second {
			t.Fatalf("при timeout <= 0: Timeout = %v, want 30s", c.Timeout)
		}
	}
	if a.Transport == b.Transport {
		t.Fatal("два вызова NewClient вернули один и тот же транспорт")
	}
}

// Под нагрузкой соединений не больше MaxConnsPerHost, а после пика
// простаивающие не закрываются — новых соединений тоже не больше лимита.
func TestMaxConnsPerHostUnderLoad(t *testing.T) {
	var active, peak, served atomic.Int64
	srv, conns := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		served.Add(1)
		io.WriteString(w, `{"sku":"C-3","qty":1}`)
		active.Add(-1)
	})
	c := client(t, 4)
	var wg sync.WaitGroup
	var failed atomic.Int64
	for range 32 {
		wg.Go(func() {
			for range 5 {
				var got item
				if err := GetJSON(context.Background(), c, srv.URL, &got); err != nil || got.SKU != "C-3" {
					failed.Add(1)
				}
			}
		})
	}
	wg.Wait()
	if f := failed.Load(); f != 0 {
		t.Fatalf("%d из 160 запросов завершились ошибкой", f)
	}
	if s := served.Load(); s != 160 {
		t.Fatalf("сервер обслужил %d запросов, want 160", s)
	}
	if n := conns.Load(); n < 1 || n > 4 {
		t.Fatalf("сервер принял %d соединений при MaxConnsPerHost = 4", n)
	}
	if p := peak.Load(); p > 4 {
		t.Fatalf("одновременно обрабатывалось %d запросов при MaxConnsPerHost = 4", p)
	}
}

func TestTooLargeByContentLength(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		body := `{"sku":"D-4","qty":1}` + strings.Repeat(" ", 2<<20)
		w.Header().Set("Content-Length", strconv.Itoa(len(body)))
		io.WriteString(w, body)
	})
	got := item{SKU: "untouched"}
	err := GetJSON(context.Background(), client(t, 2), srv.URL, &got)
	if !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
	if got.SKU != "untouched" {
		t.Fatalf("dst изменён: %+v — тело не должно читаться", got)
	}
}

// Без Content-Length (chunked) лимит MaxBody действует при чтении:
// JSON-строка длиннее мегабайта не помещается и декодируется с ошибкой.
func TestChunkedBodyLimited(t *testing.T) {
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `"`)
		w.(http.Flusher).Flush()
		io.WriteString(w, strings.Repeat("a", 3<<19)+`"`)
	})
	var s string
	err := GetJSON(context.Background(), client(t, 2), srv.URL, &s)
	var se *StatusError
	if err == nil || errors.As(err, &se) {
		t.Fatalf("err = %v, want ошибку декодирования (тело ограничено MaxBody)", err)
	}
	if len(s) > MaxBody {
		t.Fatalf("прочитано %d байт при MaxBody = %d", len(s), MaxBody)
	}
}

func TestInvalidJSONStillReuses(t *testing.T) {
	srv, conns := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{oops`+tail)
	})
	c := client(t, 2)
	for i := range 10 {
		var got item
		err := GetJSON(context.Background(), c, srv.URL, &got)
		var se *StatusError
		if err == nil || errors.As(err, &se) {
			t.Fatalf("запрос %d: err = %v, want ошибку декодирования", i, err)
		}
	}
	if n := conns.Load(); n != 1 {
		t.Fatalf("10 запросов с битым JSON открыли %d соединений, want 1", n)
	}
}

func TestMixedResponsesOneConnection(t *testing.T) {
	var n atomic.Int64
	srv, conns := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		switch n.Add(1) % 4 {
		case 0:
			io.WriteString(w, `{"sku":"E-5","qty":2}`)
		case 1:
			w.WriteHeader(http.StatusServiceUnavailable)
			io.WriteString(w, tail)
		case 2:
			w.WriteHeader(299)
			io.WriteString(w, `{"sku":"E-5","qty":2}`+"\n"+tail)
		default:
			w.WriteHeader(http.StatusMultipleChoices)
			io.WriteString(w, `{"sku":"nope"}`)
		}
	})
	c := client(t, 2)
	for i := 1; i <= 24; i++ {
		var got item
		err := GetJSON(context.Background(), c, srv.URL, &got)
		var se *StatusError
		switch i % 4 {
		case 0, 2:
			if err != nil || got.SKU != "E-5" {
				t.Fatalf("запрос %d: err = %v, got %+v", i, err, got)
			}
		case 1:
			if !errors.As(err, &se) || se.Code != 503 {
				t.Fatalf("запрос %d: err = %v, want *StatusError{503}", i, err)
			}
		case 3:
			if !errors.As(err, &se) || se.Code != 300 {
				t.Fatalf("запрос %d: err = %v, want *StatusError{300}", i, err)
			}
		}
	}
	if got := conns.Load(); got != 1 {
		t.Fatalf("24 запроса открыли %d соединений, want 1", got)
	}
}

func TestClientTimeout(t *testing.T) {
	release := make(chan struct{})
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	t.Cleanup(func() { close(release) })
	c := NewClient(2, 100*time.Millisecond)
	t.Cleanup(c.CloseIdleConnections)
	var got item
	err := GetJSON(context.Background(), c, srv.URL, &got)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want таймаут клиента (errors.Is context.DeadlineExceeded)", err)
	}
}

func TestCanceledContext(t *testing.T) {
	var hits atomic.Int64
	srv, _ := countingServer(t, func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		io.WriteString(w, `{}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var got item
	err := GetJSON(ctx, client(t, 2), srv.URL, &got)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
	if hits.Load() != 0 {
		t.Fatal("запрос с отменённым контекстом дошёл до сервера")
	}
}
