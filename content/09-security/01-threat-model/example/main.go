// Аутентификация отвечает на вопрос «кто это», авторизация —
// «можно ли ему вот это». IDOR (в OWASP API Top 10 — BOLA,
// API1:2023) возникает, когда второй вопрос никто не задал:
// сервер узнал пользователя и отдал ему объект по `id` из URL,
// не проверив, чей это объект.

package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"time"
)

type Note struct {
	ID    int
	Owner string
	Text  string
}

// Данные — в памяти и только читаются, поэтому без мьютекса.
// Токены здесь — константы для примера; настоящие выдаются
// из `crypto/rand`, а в хранилище лежит их хеш (урок 07).
var (
	notes = map[int]Note{
		1: {1, "alice", "пин-код от сейфа: 4721"},
		2: {2, "bob", "купить молоко"},
	}
	tokens = map[string]string{"t-alice": "alice", "t-bob": "bob"}
)

type userKey struct{}

// Middleware аутентификации узнаёт пользователя по токену и
// кладёт имя в контекст; без токена — 401. Это аналог
// `[Authorize]`: пользователь известен, но о заметках
// middleware ничего не знает и знать не может.
func authn(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
		user, known := tokens[token]
		if !ok || !known {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), userKey{}, user)))
	})
}

func currentUser(r *http.Request) string {
	user, _ := r.Context().Value(userKey{}).(string)
	return user
}

// УЯЗВИМО: проверена только аутентификация. Любой вошедший
// пользователь перебирает `id` и читает чужие заметки.
func getNoteUnsafe(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	n, ok := notes[id]
	if !ok {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fmt.Fprint(w, n.Text)
}

// Исправление: владелец проверяется в хендлере, сразу после
// загрузки объекта. Чужая заметка неотличима от
// несуществующей — 404, а не 403, так не узнать, какие `id`
// заняты.
func getNote(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.Atoi(r.PathValue("id"))
	n, ok := notes[id]
	if !ok || n.Owner != currentUser(r) {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	fmt.Fprint(w, n.Text)
}

// Обе версии — на одном сервере на loopback, с таймаутом
// чтения заголовков. Клиент — тоже с таймаутом.
func main() {
	mux := http.NewServeMux()
	mux.Handle("GET /v1/notes/{id}", authn(http.HandlerFunc(getNoteUnsafe)))
	mux.Handle("GET /v2/notes/{id}", authn(http.HandlerFunc(getNote)))
	srv := httptest.NewUnstartedServer(mux)
	srv.Config.ReadHeaderTimeout = 5 * time.Second
	srv.Start()
	defer srv.Close()
	client := &http.Client{Timeout: 5 * time.Second}

	// Заметка 1 принадлежит Алисе. Боб аутентифицирован —
	// и через `/v1` читает её, просто подставив чужой `id`.
	for _, c := range []struct{ who, path string }{
		{"alice", "/v2/notes/1"},
		{"аноним", "/v2/notes/1"},
		{"bob", "/v1/notes/1"},
		{"bob", "/v2/notes/1"},
		{"bob", "/v2/notes/2"},
	} {
		req, _ := http.NewRequest("GET", srv.URL+c.path, nil)
		if c.who != "аноним" {
			req.Header.Set("Authorization", "Bearer t-"+c.who)
		}
		resp, err := client.Do(req)
		if err != nil {
			fmt.Println("ошибка:", err)
			return
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%-7s GET %s → %d %s\n", c.who, c.path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
}
