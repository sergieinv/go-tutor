// Мягкая остановка сервера: перестать принимать новые
// соединения, дать активным запросам доработать, потом
// закрыть зависимости. Здесь `SIGTERM` посылаем сами себе —
// так делают `docker stop` и Kubernetes.

package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync/atomic"
	"syscall"
	"time"
)

func main() {
	// Контекст отменится по SIGINT или SIGTERM.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Флаг готовности: балансировщик спрашивает `/ready`
	// и перестаёт слать трафик, когда получает 503.
	var ready atomic.Bool
	ready.Store(true)

	started, release := make(chan struct{}), make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /ready", func(w http.ResponseWriter, r *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	})
	mux.HandleFunc("GET /slow", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		<-release // долгая работа
		fmt.Fprint(w, "отчёт готов")
	})

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: time.Second}
	// Хуки для hijacked-соединений (WebSocket): `Shutdown`
	// их не ждёт, закрывать их — ваша забота.
	hooked := make(chan struct{})
	srv.RegisterOnShutdown(func() { close(hooked) })

	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	// Запрос, который застанет остановку.
	slow := make(chan string, 1)
	go func() {
		resp, err := client.Get(base + "/slow")
		if err != nil {
			slow <- err.Error()
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		slow <- fmt.Sprint(resp.StatusCode, " ", string(body))
	}()
	<-started

	syscall.Kill(os.Getpid(), syscall.SIGTERM)
	<-ctx.Done()
	fmt.Println("сигнал получен:", ctx.Err())

	// Сначала снимаем готовность (в проде — и пауза в пару
	// секунд, пока балансировщик это заметит).
	ready.Store(false)
	resp, _ := client.Get(base + "/ready")
	resp.Body.Close()
	fmt.Println("/ready →", resp.StatusCode)

	// Таймаут на остановку — от `Background`: `ctx` уже отменён.
	shCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- srv.Shutdown(shCtx) }()

	// `Serve` возвращается сразу, когда закрыты слушатели, —
	// запрос ещё выполняется. Поэтому `main` ждёт `Shutdown`.
	err := <-serveErr
	fmt.Println("Serve вернул:", err, "— а запрос ещё идёт")
	<-hooked
	fmt.Println("хуки RegisterOnShutdown вызваны")
	close(release)

	fmt.Println("Shutdown вернул:", <-shutdownDone)
	fmt.Println("медленный запрос:", <-slow)
	_, err = client.Get(base + "/ready")
	fmt.Println("новый запрос отклонён:", err != nil)

	// Зависимости — после сервера: хендлеры могли ими пользоваться.
	fmt.Println("закрываем базу данных")
}
