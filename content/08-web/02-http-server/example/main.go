// Сервер в Go — обычная программа: `http.Server`, сокет
// и любое значение, реализующее `http.Handler`. Ни хоста,
// ни конвейера, ни DI — всё собирается в `main`.

package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Хендлер — любой тип с методом `ServeHTTP`. Каждый запрос
// обслуживается в своей горутине, поэтому общее состояние
// защищаем мьютексом.
type counter struct {
	mu   sync.Mutex
	hits int
}

func (c *counter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	c.mu.Lock()
	c.hits++
	n := c.hits
	c.mu.Unlock()
	// Заголовки — до первой записи тела или `WriteHeader`.
	// Без `WriteHeader` статус будет 200.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	fmt.Fprintf(w, "визит %d\n", n)
}

// `http.HandlerFunc` превращает функцию в `Handler`.
func create(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		// `http.Error` ставит text/plain, статус и тело.
		w.Header().Set("Allow", "POST")
		http.Error(w, "только POST", http.StatusMethodNotAllowed)
		return // без return код ниже допишет ответ
	}
	w.Header().Set("Location", "/items/1")
	w.WriteHeader(http.StatusCreated)
	w.Header().Set("X-Late", "1")                 // поздно: заголовки уже ушли
	w.WriteHeader(http.StatusInternalServerError) // superfluous — игнорируется
	io.WriteString(w, "создано\n")
}

func main() {
	mux := http.NewServeMux()
	mux.Handle("/visits", &counter{})
	mux.Handle("/items", http.HandlerFunc(create))

	// Таймауты обязательны: без `ReadHeaderTimeout` клиент,
	// присылающий заголовки по байту, держит соединение вечно.
	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 2 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    64 << 10,
		ErrorLog:          log.New(lineCutter{}, "", 0),
	}
	// Порт 0 — свободный порт от ОС. `Serve` блокируется,
	// поэтому запускаем его в горутине.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		log.Fatal(err)
	}
	go srv.Serve(ln)
	base := "http://" + ln.Addr().String()

	client := &http.Client{Timeout: 5 * time.Second}
	for _, req := range [][2]string{{"GET", "/visits"}, {"GET", "/visits"}, {"GET", "/items"}, {"POST", "/items"}} {
		r, _ := http.NewRequest(req[0], base+req[1], nil)
		resp, err := client.Do(r)
		if err != nil {
			log.Fatal(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%s %s → %d %q Location=%q X-Late=%q\n", req[0], req[1],
			resp.StatusCode, body, resp.Header.Get("Location"), resp.Header.Get("X-Late"))
	}

	// Остановка: `Shutdown` ждёт завершения активных запросов.
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	fmt.Println("shutdown:", srv.Shutdown(ctx))
}

// lineCutter печатает сообщения журнала сервера без
// ссылки на строку исходника.
type lineCutter struct{}

func (lineCutter) Write(p []byte) (int, error) {
	msg, _, _ := strings.Cut(string(p), " from ")
	fmt.Println("журнал сервера:", msg)
	return len(p), nil
}
