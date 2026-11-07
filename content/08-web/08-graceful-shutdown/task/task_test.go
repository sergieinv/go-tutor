package task

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"testing"
	"time"
)

func listen(t *testing.T) net.Listener {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	return ln
}

// runAsync запускает Run в горутине и возвращает канал с её результатом.
func runAsync(ctx context.Context, srv *http.Server, ln net.Listener, timeout time.Duration) <-chan error {
	done := make(chan error, 1)
	go func() { done <- Run(ctx, srv, ln, timeout) }()
	return done
}

func waitRun(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("Run не вернулся за 3 с")
		return nil
	}
}

var client = &http.Client{Timeout: 3 * time.Second}

func get(url string) (string, error) {
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	return string(b), err
}

// getAsync выполняет GET в горутине; результат — тело или текст ошибки.
func getAsync(url string) <-chan string {
	res := make(chan string, 1)
	go func() {
		body, err := get(url)
		if err != nil {
			body = "error: " + err.Error()
		}
		res <- body
	}()
	return res
}

// mustGet выполняет GET, пока Run работает; если Run вернулся раньше — тест падает.
func mustGet(t *testing.T, url string, done <-chan error) string {
	t.Helper()
	select {
	case body := <-getAsync(url):
		return body
	case err := <-done:
		t.Fatalf("Run вернул %v, хотя ctx не отменён", err)
	case <-time.After(3 * time.Second):
		t.Fatal("сервер не ответил за 3 с")
	}
	return ""
}

// waitStarted ждёт, пока запрос дойдёт до хендлера.
func waitStarted(t *testing.T, started <-chan struct{}, done <-chan error) {
	t.Helper()
	select {
	case <-started:
	case err := <-done:
		t.Fatalf("Run вернул %v, хотя ctx не отменён", err)
	case <-time.After(3 * time.Second):
		t.Fatal("запрос не дошёл до хендлера за 3 с")
	}
}

func TestServesUntilCanceled(t *testing.T) {
	ln := listen(t)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "pong")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, srv, ln, time.Second)

	if body := mustGet(t, "http://"+ln.Addr().String(), done); body != "pong" {
		t.Fatalf("GET = %q, want pong", body)
	}
	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

func TestWaitsForActiveRequest(t *testing.T) {
	ln := listen(t)
	started, release := make(chan struct{}), make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		io.WriteString(w, "done")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, srv, ln, 2*time.Second)

	resp := getAsync("http://" + ln.Addr().String())
	waitStarted(t, started, done)
	cancel()
	select {
	case err := <-done:
		t.Fatalf("Run вернул %v, пока запрос ещё выполняется", err)
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
	if body := <-resp; body != "done" {
		t.Fatalf("ответ активного запроса = %q, want done", body)
	}
}

func TestShutdownTimeout(t *testing.T) {
	ln := listen(t)
	started := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done() // долгий опрос
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, srv, ln, 50*time.Millisecond)
	getAsync("http://" + ln.Addr().String())
	waitStarted(t, started, done)
	cancel()
	err := waitRun(t, done)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want ошибку с context.DeadlineExceeded", err)
	}
}
