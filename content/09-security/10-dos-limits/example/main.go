// Отказ в обслуживании редко требует ботнета: хватает
// клиента, который держит соединения открытыми, шлёт
// гигабайты или сжатую «бомбу». У `http.Server` все
// таймауты по умолчанию нулевые, тело не ограничено —
// лимиты ставит программист, на каждом уровне свой.

package main

import (
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"time"
)

// УЯЗВИМО: ни одного таймаута. Клиент, который шлёт
// заголовки по байту в минуту (Slowloris), держит
// горутину и сокет сколько угодно; тысяча таких — и
// кончатся файловые дескрипторы.
func newServerUnsafe(h http.Handler) *http.Server {
	return &http.Server{Handler: h}
}

// `ReadHeaderTimeout` — главный против Slowloris: сервер
// закрывает соединение, если заголовки не пришли вовремя.
// `ReadTimeout`/`WriteTimeout` ограничивают чтение запроса
// и запись ответа, `IdleTimeout` — простой keep-alive.
func newServer(h http.Handler) *http.Server {
	return &http.Server{
		Handler:           h,
		ReadHeaderTimeout: 200 * time.Millisecond,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    16 << 10,
	}
}

// Медленный клиент: начало заголовков — и тишина.
// Реакции сервера ждём одну секунду.
func slowloris(srv *http.Server) string {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err.Error()
	}
	go srv.Serve(ln)
	defer srv.Close() // Close рвёт и повисшие соединения
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		return err.Error()
	}
	defer c.Close()
	io.WriteString(c, "GET / HTTP/1.1\r\nHost: shop\r\n")
	c.SetReadDeadline(time.Now().Add(time.Second))
	_, err = c.Read(make([]byte, 1))
	switch {
	case errors.Is(err, io.EOF):
		return "сервер закрыл соединение"
	case errors.Is(err, os.ErrDeadlineExceeded):
		return "соединение висит"
	}
	return fmt.Sprint(err)
}

// Распаковка с лимитом: читаем не больше `max+1` байт.
// Если прочитался лишний байт — данных больше лимита,
// это бомба (или просто слишком большой запрос).
func gunzipLimited(r io.Reader, max int64) ([]byte, error) {
	zr, err := gzip.NewReader(r)
	if err != nil {
		return nil, err
	}
	defer zr.Close()
	data, err := io.ReadAll(io.LimitReader(zr, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("распакованные данные больше %d байт", max)
	}
	return data, nil
}

// Тело запроса — через `http.MaxBytesReader`: сверх лимита
// чтение вернёт `*http.MaxBytesError`, а сервер закроет
// соединение после ответа, не дочитывая остаток.
func upload(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	n, err := io.Copy(io.Discard, r.Body)
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		http.Error(w, "тело больше 1 КиБ", http.StatusRequestEntityTooLarge)
		return
	}
	fmt.Fprintf(w, "принято %d байт\n", n)
}

// Лимит одновременных обработок: семафор на буферизованном
// канале. Слота нет — сразу 503 и `Retry-After`, а не
// очередь, которая съест память.
func limit(n int, next http.Handler) http.Handler {
	slots := make(chan struct{}, n)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
			next.ServeHTTP(w, r)
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "сервер занят", http.StatusServiceUnavailable)
		}
	})
}

func main() {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	fmt.Println("без таймаутов:", slowloris(newServerUnsafe(ok)))
	fmt.Println("ReadHeaderTimeout:", slowloris(newServer(ok)))

	for _, size := range []int{100, 5000} {
		req := httptest.NewRequest("POST", "/upload", strings.NewReader(strings.Repeat("x", size)))
		rec := httptest.NewRecorder()
		upload(rec, req)
		fmt.Print(rec.Code, " ", rec.Body.String())
	}

	// 10 МиБ нулей сжимаются в несколько килобайт: лимит
	// на тело запроса бомбу пропустит, нужен второй —
	// на распакованный размер.
	var bomb bytes.Buffer
	zw := gzip.NewWriter(&bomb)
	zw.Write(make([]byte, 10<<20))
	zw.Close()
	fmt.Println("сжатая бомба меньше 32 КиБ:", bomb.Len() < 32<<10)
	_, err := gunzipLimited(&bomb, 1<<20)
	fmt.Println("gunzip:", err)

	// Первый запрос занимает единственный слот и ждёт,
	// второй в это время получает 503.
	entered, release := make(chan struct{}), make(chan struct{})
	h := limit(1, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(entered)
		<-release
	}))
	done := make(chan int)
	go func() {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
		done <- rec.Code
	}()
	<-entered
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	fmt.Println("второй запрос:", rec.Code, "Retry-After:", rec.Header().Get("Retry-After"))
	close(release)
	fmt.Println("первый запрос:", <-done)
}
