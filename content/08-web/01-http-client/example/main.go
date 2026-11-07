// HTTP-клиент Go — `http.Client` из стандартной библиотеки.
// Тестовый сервер здесь — `httptest.NewServer`: он слушает
// случайный порт на `127.0.0.1`, как настоящий.

package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

func main() {
	mux := http.NewServeMux()
	mux.HandleFunc("/user", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"name":"Анна","agent":%q}`, r.Header.Get("User-Agent"))
	})
	mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done() // ждёт, пока клиент не сдастся
	})
	mux.HandleFunc("/big", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", 10_000))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	// `http.DefaultClient` (его используют `http.Get` и
	// `http.Post`) не имеет таймаута: зависший сервер
	// подвесит вызов навсегда. Свой клиент — с `Timeout`
	// на весь запрос, включая чтение тела.
	fmt.Println("DefaultClient.Timeout =", http.DefaultClient.Timeout)
	client := &http.Client{Timeout: 5 * time.Second}

	// Запрос с контекстом и заголовками. `Do` возвращает
	// ошибку только при сбое сети, таймауте или отмене.
	ctx := context.Background()
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/user", nil)
	req.Header.Set("User-Agent", "go-tutor/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println("ошибка:", err)
		return
	}
	// Тело закрываем всегда — иначе утечёт соединение.
	defer resp.Body.Close()
	var user struct{ Name, Agent string }
	err = json.NewDecoder(resp.Body).Decode(&user)
	fmt.Println(resp.StatusCode, resp.Header.Get("Content-Type"), user, err)

	// 404 — не ошибка `err`, а обычный ответ: статус
	// проверяем сами. Тело дочитываем и закрываем, чтобы
	// соединение вернулось в пул и пригодилось снова.
	resp, err = client.Get(srv.URL + "/missing")
	fmt.Println("err:", err, "статус:", resp.Status)
	io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	// Дедлайн через контекст. Ошибка `*url.Error` оборачивает
	// причину: `errors.Is` находит `DeadlineExceeded`.
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	req, _ = http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/slow", nil)
	_, err = client.Do(req)
	fmt.Println("дедлайн:", errors.Is(err, context.DeadlineExceeded))

	// Не доверяем размеру ответа: читаем не больше лимита
	// плюс один байт — так видно, что лимит превышен.
	resp, err = client.Get(srv.URL + "/big")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	const limit = 4096
	body, _ := io.ReadAll(io.LimitReader(resp.Body, limit+1))
	fmt.Println("прочитано:", len(body), "больше лимита:", len(body) > limit)
}
