// Middleware в Go — функция `func(http.Handler) http.Handler`:
// принимает следующий хендлер и возвращает обёртку.
// Никакого конвейера: цепочка — это вложенные вызовы.

package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"sync/atomic"
	"time"
)

// Обёртка `ResponseWriter` запоминает статус и размер.
// `Unwrap` нужен `http.NewResponseController`, чтобы
// добраться до `Flush` и дедлайнов исходного writer.
type statusWriter struct {
	http.ResponseWriter
	status, size int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK // неявный 200
	}
	n, err := w.ResponseWriter.Write(b)
	w.size += n
	return n, err
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Логирование — снаружи: видит итоговый статус, даже
// если ответ написал recover. id запроса берём из
// заголовка ответа — контекст внутренних слоёв ему не виден.
func logging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w}
		next.ServeHTTP(sw, r)
		if sw.status == 0 {
			sw.status = http.StatusOK // ничего не записано — net/http ответит 200
		}
		fmt.Printf("лог: %s %s %s → %d, %d байт\n",
			w.Header().Get("X-Request-ID"), r.Method, r.URL.Path, sw.status, sw.size)
	})
}

// Паника в хендлере не должна оставить клиента без ответа.
// `http.ErrAbortHandler` — штатный способ оборвать ответ,
// его пробрасываем дальше.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if v := recover(); v != nil {
				if v == http.ErrAbortHandler {
					panic(v)
				}
				fmt.Println("recover:", v)
				http.Error(w, "internal error", http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type ctxKey struct{}

var lastID atomic.Int64

// id запроса — в заголовок ответа и в контекст.
// `WithContext` создаёт копию запроса.
func requestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := "req-" + strconv.FormatInt(lastID.Add(1), 10)
		w.Header().Set("X-Request-ID", id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

var slowStarted = make(chan struct{})

// Хендлер не знает о middleware: id берёт из контекста,
// а при обрыве соединения видит `ctx.Done()`.
func getOrder(w http.ResponseWriter, r *http.Request) {
	id, ctx := r.PathValue("id"), r.Context()
	switch id {
	case "13":
		panic("заказ 13 сломан")
	case "slow":
		close(slowStarted)
		select {
		case <-time.After(10 * time.Second):
		case <-ctx.Done():
			fmt.Println("хендлер: клиент ушёл:", ctx.Err())
			return
		}
	}
	fmt.Fprintf(w, "заказ %s (%s)\n", id, ctx.Value(ctxKey{}))
}

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /orders/{id}", getOrder)

	// Кто снаружи — тот выполняется первым.
	handler := logging(recoverer(requestID(mux)))

	srv := &http.Server{Handler: handler, ReadHeaderTimeout: time.Second}
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	go srv.Serve(ln)
	base := "http://" + ln.Addr().String()
	client := &http.Client{Timeout: 5 * time.Second}

	for _, id := range []string{"42", "13"} {
		resp, err := client.Get(base + "/orders/" + id)
		if err != nil {
			panic(err)
		}
		resp.Body.Close()
		fmt.Println("клиент:", resp.Status)
	}

	// Клиент сдаётся, когда хендлер уже работает, и рвёт
	// соединение.
	ctx, cancel := context.WithCancel(context.Background())
	go func() { <-slowStarted; cancel() }()
	req, _ := http.NewRequestWithContext(ctx, "GET", base+"/orders/slow", nil)
	_, err := client.Do(req)

	// `Shutdown` ждёт, пока хендлер и логирование закончат.
	srv.Shutdown(context.Background())
	fmt.Println("клиент: ошибка", ctx.Err(), err != nil)
}
