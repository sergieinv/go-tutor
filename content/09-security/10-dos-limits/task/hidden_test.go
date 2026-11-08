package task

import (
	"bufio"
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestExactLimitsAccepted(t *testing.T) {
	l := testLimits()
	body := bytes.Repeat([]byte("a"), int(l.MaxBodyBytes))
	if rec := post(Protect(&echo{}, l), body, ""); rec.Code != 200 || rec.Body.Len() != len(body) {
		t.Errorf("тело ровно MaxBodyBytes: %d, %d байт, want 200 и всё тело", rec.Code, rec.Body.Len())
	}
	plain := bytes.Repeat([]byte("b"), int(l.MaxDecompressed))
	if rec := post(Protect(&echo{}, l), gz(t, plain), "gzip"); rec.Code != 200 || rec.Body.Len() != len(plain) {
		t.Errorf("распаковано ровно MaxDecompressed: %d, %d байт, want 200 и всё тело", rec.Code, rec.Body.Len())
	}
}

func TestCompressedBodyLimit(t *testing.T) {
	// Случайные данные не сжимаются: сжатое тело больше MaxBodyBytes.
	l := testLimits()
	noise := make([]byte, 3<<10)
	rand.Read(noise)
	if rec := post(Protect(&echo{}, l), gz(t, noise), "gzip"); rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("сжатое тело больше MaxBodyBytes: код %d, want 413", rec.Code)
	}
}

func TestSlotsReleased(t *testing.T) {
	l := testLimits()
	l.MaxInFlight = 1
	e := &echo{}
	h := Protect(e, l)
	post(h, []byte("x"), "br")          // отказ до next
	post(h, []byte("not gzip"), "gzip") // отказ до next
	for i := range 3 {
		if rec := post(h, []byte("x"), ""); rec.Code != 200 {
			t.Fatalf("запрос %d после завершения предыдущих: код %d, want 200 — слот не освобождён?", i+1, rec.Code)
		}
	}
}

func TestExploitHugeBody(t *testing.T) {
	l := testLimits()
	rec := post(Protect(&echo{}, l), bytes.Repeat([]byte("a"), 64<<10), "")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("тело 64 КиБ при лимите 1 КиБ: код %d, want 413 — хендлер прочитал всё", rec.Code)
	}
}

func TestExploitGzipBomb(t *testing.T) {
	l := testLimits()
	bomb := gz(t, make([]byte, 8<<20)) // 8 МиБ нулей → несколько КиБ
	if len(bomb) > 64<<10 {
		t.Fatalf("бомба неожиданно велика: %d байт", len(bomb))
	}
	l.MaxBodyBytes = int64(len(bomb)) + 1 // сжатое тело в лимите
	rec := post(Protect(&echo{}, l), bomb, "gzip")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("бомба 8 МиБ при MaxDecompressed 4 КиБ: код %d, want 413", rec.Code)
	}
	var n int64
	if _, err := fmt.Sscanf(rec.Body.String(), "too large: read %d", &n); err != nil || n > l.MaxDecompressed {
		t.Errorf("хендлер должен получить не больше MaxDecompressed байт до ошибки: %q", rec.Body.String())
	}
}

func TestExploitFlood(t *testing.T) {
	l := testLimits() // MaxInFlight: 2
	entered := make(chan struct{}, 10)
	release := make(chan struct{})
	h := Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
	}), l)

	var wg sync.WaitGroup
	codes := make(chan int, 3)
	serve := func() {
		defer wg.Done()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		codes <- rec.Code
	}
	wg.Add(2)
	go serve()
	go serve()
	for range 2 {
		select {
		case <-entered:
		case <-time.After(5 * time.Second):
			close(release)
			t.Fatal("два запроса при MaxInFlight=2 не дошли до хендлера")
		}
	}

	wg.Add(1)
	go serve()
	select {
	case code := <-codes:
		if code != http.StatusServiceUnavailable {
			t.Errorf("третий запрос: код %d, want 503", code)
		}
	case <-entered:
		t.Errorf("третий запрос дошёл до хендлера при двух занятых слотах")
	case <-time.After(5 * time.Second):
		t.Errorf("третий запрос ждёт слот, а должен сразу получить 503")
	}
	close(release)
	wg.Wait()
}

func TestRetryAfter(t *testing.T) {
	l := testLimits()
	l.MaxInFlight = 1
	entered, release := make(chan struct{}, 2), make(chan struct{})
	h := Protect(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		<-release
	}), l)
	done := make(chan struct{})
	go func() {
		defer close(done)
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/", nil))
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("первый запрос не дошёл до хендлера")
	}
	rec := httptest.NewRecorder()
	ch := make(chan struct{})
	go func() {
		defer close(ch)
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	}()
	select {
	case <-ch:
	case <-time.After(time.Second): // без лимита второй запрос висит — ждём с запасом, но недолго
		close(release)
		<-done
		t.Fatal("второй запрос ждёт слот, а должен сразу получить 503")
	}
	close(release)
	<-done
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") != "1" {
		t.Fatalf("второй запрос: %d Retry-After=%q, want 503 и \"1\"", rec.Code, rec.Header().Get("Retry-After"))
	}
}

// serve запускает сервер из NewServer на loopback.
func serve(t *testing.T, l Limits) string {
	t.Helper()
	srv := NewServer(&echo{}, l)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

func TestExploitSlowloris(t *testing.T) {
	l := testLimits()
	l.HeaderTimeout = 100 * time.Millisecond
	c, err := net.Dial("tcp", serve(t, l))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	io.WriteString(c, "GET / HTTP/1.1\r\nHost: x\r\n") // и тишина
	c.SetReadDeadline(time.Now().Add(time.Second))
	_, err = c.Read(make([]byte, 1))
	if errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatal("медленный клиент держит соединение 1 с при HeaderTimeout 100 мс")
	}
}

func TestExploitHugeHeaders(t *testing.T) {
	l := testLimits()
	l.MaxHeaderBytes = 1 << 10
	c, err := net.Dial("tcp", serve(t, l))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(5 * time.Second))
	go io.WriteString(c, "GET / HTTP/1.1\r\nHost: x\r\nX-Junk: "+strings.Repeat("j", 64<<10)+"\r\n\r\n")
	resp, err := http.ReadResponse(bufio.NewReader(c), nil)
	if err != nil {
		t.Fatalf("нет ответа: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusRequestHeaderFieldsTooLarge {
		t.Fatalf("заголовки 64 КиБ при MaxHeaderBytes 1 КиБ: %s, want 431", resp.Status)
	}
}
