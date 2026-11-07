package task

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"runtime"
	"testing"
	"time"
)

func TestRequestContextSurvivesSignal(t *testing.T) {
	// Сигнал остановки не должен отменять контексты активных
	// запросов: они дорабатывают в отведённое время.
	ln := listen(t)
	started, release := make(chan struct{}), make(chan struct{})
	ctxErr := make(chan error, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release
		ctxErr <- r.Context().Err()
		io.WriteString(w, "ok")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, srv, ln, 2*time.Second)
	getAsync("http://" + ln.Addr().String())
	waitStarted(t, started, done)
	cancel()
	time.Sleep(50 * time.Millisecond)
	close(release)
	if err := <-ctxErr; err != nil {
		t.Fatalf("контекст запроса отменён во время мягкой остановки: %v — BaseContext не должен зависеть от ctx", err)
	}
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run = %v, want nil", err)
	}
}

func TestTimeoutCancelsRequestContexts(t *testing.T) {
	ln := listen(t)
	started := make(chan struct{})
	observed := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-r.Context().Done()
		close(observed)
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, srv, ln, 50*time.Millisecond)
	getAsync("http://" + ln.Addr().String())
	waitStarted(t, started, done)
	start := time.Now()
	cancel()
	err := waitRun(t, done)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run = %v, want ошибку с context.DeadlineExceeded", err)
	}
	if time.Since(start) < 50*time.Millisecond {
		t.Errorf("Run вернулся раньше timeout")
	}
	select {
	case <-observed:
	case <-time.After(time.Second):
		t.Fatal("после истечения timeout контекст запроса не отменён")
	}
	if srv.BaseContext == nil {
		t.Fatal("Run должен задать srv.BaseContext")
	}
	if srv.BaseContext(ln).Err() == nil {
		t.Error("после истечения timeout контекст из BaseContext не отменён")
	}
}

func TestRefusesAfterRun(t *testing.T) {
	ln := listen(t)
	addr := ln.Addr().String()
	srv := &http.Server{Handler: http.NotFoundHandler()}
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, srv, ln, time.Second)
	mustGet(t, "http://"+addr, done)
	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run = %v", err)
	}
	if c, err := net.DialTimeout("tcp", addr, time.Second); err == nil {
		c.Close()
		t.Fatal("после Run слушатель ещё принимает соединения")
	}
}

func TestServeError(t *testing.T) {
	ln := listen(t)
	ln.Close() // Serve сразу вернёт ошибку
	err := waitRun(t, runAsync(context.Background(), &http.Server{}, ln, time.Second))
	if !errors.Is(err, net.ErrClosed) {
		t.Fatalf("Run = %v, want ошибку Serve (net.ErrClosed), не дожидаясь ctx", err)
	}
}

func TestShutdownByOther(t *testing.T) {
	ln := listen(t)
	srv := &http.Server{Handler: http.NotFoundHandler()}
	done := runAsync(context.Background(), srv, ln, time.Second)
	mustGet(t, "http://"+ln.Addr().String(), done)
	srv.Shutdown(context.Background())
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run после внешнего Shutdown = %v, want nil (ErrServerClosed — не ошибка)", err)
	}
}

func TestAlreadyCanceled(t *testing.T) {
	ln := listen(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitRun(t, runAsync(ctx, &http.Server{}, ln, time.Second)); err != nil {
		t.Fatalf("Run с отменённым ctx = %v, want nil", err)
	}
	if c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second); err == nil {
		c.Close()
		t.Fatal("после Run слушатель ещё принимает соединения")
	}
}

func TestZeroTimeoutWaits(t *testing.T) {
	ln := listen(t)
	started := make(chan struct{})
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(started)
		time.Sleep(100 * time.Millisecond)
		io.WriteString(w, "slow")
	})}
	ctx, cancel := context.WithCancel(context.Background())
	done := runAsync(ctx, srv, ln, 0)
	resp := getAsync("http://" + ln.Addr().String())
	waitStarted(t, started, done)
	cancel()
	if err := waitRun(t, done); err != nil {
		t.Fatalf("Run(timeout=0) = %v, want nil: при timeout <= 0 лимита нет", err)
	}
	if b := <-resp; b != "slow" {
		t.Fatalf("ответ = %q, want slow", b)
	}
}

func TestNoGoroutineLeak(t *testing.T) {
	client.CloseIdleConnections()
	before := runtime.NumGoroutine()
	for range 5 {
		ln := listen(t)
		srv := &http.Server{Handler: http.NotFoundHandler()}
		ctx, cancel := context.WithCancel(context.Background())
		done := runAsync(ctx, srv, ln, time.Second)
		mustGet(t, "http://"+ln.Addr().String(), done)
		cancel()
		waitRun(t, done)
	}
	client.CloseIdleConnections()
	if !waitFor(time.Second, func() bool { return runtime.NumGoroutine() <= before }) {
		t.Errorf("горутин до: %d, после: %d — Run должен дождаться завершения Serve", before, runtime.NumGoroutine())
	}
}

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}
