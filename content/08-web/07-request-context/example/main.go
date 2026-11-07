// У каждого запроса есть контекст `r.Context()`. Сервер
// отменяет его, когда клиент разорвал соединение или когда
// `ServeHTTP` вернулся. Хендлер передаёт его во все
// зависимые вызовы — так брошенная работа останавливается.

package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
)

// Имитация запроса к базе: уважает отмену и дедлайн.
func slowQuery(ctx context.Context, d time.Duration) error {
	select {
	case <-time.After(d):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Ключ значения — неэкспортируемый тип. В контексте —
// только данные запроса: id, пользователь, трассировка.
type ctxKey struct{}

func withRequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := context.WithValue(r.Context(), ctxKey{}, r.Header.Get("X-Request-Id"))
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Дедлайн на запрос — middleware. `WithTimeoutCause`
// запоминает, почему контекст истёк: `ctx.Err()` останется
// `DeadlineExceeded`, а `context.Cause` вернёт нашу ошибку.
var errSlow = errors.New("лимит 50 мс на поиск")

func withTimeout(d time.Duration, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeoutCause(r.Context(), d, errSlow)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func main() {
	// Хендлеры сообщают о себе через канал: так вывод не
	// зависит от планировщика.
	events := make(chan string, 1)
	started := make(chan struct{})
	mux := http.NewServeMux()

	mux.HandleFunc("GET /report", func(w http.ResponseWriter, r *http.Request) {
		close(started)
		err := slowQuery(r.Context(), 10*time.Second)
		events <- fmt.Sprint("report: ", err)
	})
	mux.Handle("GET /search", withTimeout(50*time.Millisecond, http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			if err := slowQuery(r.Context(), time.Second); errors.Is(err, context.DeadlineExceeded) {
				http.Error(w, "timeout", http.StatusGatewayTimeout)
				events <- fmt.Sprint("search: ", err, " / ", context.Cause(r.Context()))
			}
		})))
	// `http.TimeoutHandler` сам отвечает 503 — даже если
	// хендлер контекст не слушает. Запись опоздавшего
	// хендлера вернёт `http.ErrHandlerTimeout`.
	mux.Handle("GET /export", http.TimeoutHandler(http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) {
			time.Sleep(300 * time.Millisecond) // ctx не слушаем
			_, werr := w.Write([]byte("csv..."))
			events <- fmt.Sprint("export: ", r.Context().Err(), " / ", werr)
		}), 50*time.Millisecond, "слишком долго"))
	// Работа после ответа: `WithoutCancel` сохраняет значения,
	// но не отменяется вместе с запросом.
	mux.HandleFunc("POST /orders", func(w http.ResponseWriter, r *http.Request) {
		bg := context.WithoutCancel(r.Context())
		go func() {
			<-r.Context().Done() // ServeHTTP вернулся
			events <- fmt.Sprint("аудит ", bg.Value(ctxKey{}), ": запрос ", r.Context().Err(), ", фон ", bg.Err())
		}()
		fmt.Fprint(w, "заказ принят")
	})

	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	srv := &http.Server{Handler: withRequestID(mux), ReadHeaderTimeout: time.Second}
	go srv.Serve(ln)
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	// Клиент передумал: отмена его контекста рвёт соединение,
	// и сервер отменяет `r.Context()`.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-started; cancel() }()
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/report", nil)
	_, err := client.Do(req)
	fmt.Println("клиент:", errors.Is(err, context.Canceled))
	fmt.Println(<-events)

	for _, path := range []string{"/search", "/export"} {
		resp, _ := client.Get(base + path)
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%s → %d %q\n", path, resp.StatusCode, body)
		fmt.Println(<-events)
	}

	req, _ = http.NewRequest("POST", base+"/orders", nil)
	req.Header.Set("X-Request-Id", "req-42")
	resp, _ := client.Do(req)
	resp.Body.Close()
	fmt.Println("/orders →", resp.StatusCode)
	fmt.Println(<-events)

	srv.Shutdown(context.Background())
}
