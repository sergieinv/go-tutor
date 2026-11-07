// С Go 1.22 стандартный `http.ServeMux` понимает метод
// и параметры пути в шаблоне — для REST чаще всего
// хватает его, без сторонних роутеров.

package main

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
)

func main() {
	mux := http.NewServeMux()

	// `{$}` — точное совпадение: только `/`, а не всё
	// дерево. Без него `/` ловит любой путь.
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "главная")
	})

	// `{id}` — один сегмент пути, значение даёт
	// `r.PathValue`. Метод `GET` включает и `HEAD`.
	mux.HandleFunc("GET /books/{id}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "книга %s (шаблон %q)", r.PathValue("id"), r.Pattern)
	})
	mux.HandleFunc("DELETE /books/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	// Литерал специфичнее параметра: `/books/new` выиграет
	// у `/books/{id}` независимо от порядка регистрации.
	mux.HandleFunc("GET /books/new", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "форма новой книги")
	})

	// `{path...}` — остаток пути вместе со слешами.
	mux.HandleFunc("GET /files/{path...}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "файл %q", r.PathValue("path"))
	})

	// Хост в шаблоне: такие маршруты проверяются первыми.
	mux.HandleFunc("GET admin.local/{$}", func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "админка")
	})

	for _, req := range []string{
		"GET /",
		"GET /books/42",
		"HEAD /books/42", // тело для HEAD отбросит сервер
		"GET /books/new",
		"GET /files/docs/2024/report.pdf",
		"GET http://admin.local/",
		"GET /about",          // 404: `/` с `{$}` не ловит всё
		"GET /books/42/pages", // 404: `{id}` — ровно один сегмент
		"PUT /books/42",       // 405 и заголовок Allow
		"GET /files",          // редирект на `/files/`
	} {
		method, target, _ := strings.Cut(req, " ")
		rec := httptest.NewRecorder()
		mux.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		res := rec.Result()
		fmt.Printf("%-32s → ", req)
		switch {
		case res.StatusCode == http.StatusMethodNotAllowed:
			fmt.Println("405, Allow:", res.Header.Get("Allow"))
		case res.StatusCode >= 300 && res.StatusCode < 400:
			fmt.Println("редирект на", res.Header.Get("Location"))
		default:
			fmt.Println(res.StatusCode, strings.TrimSpace(rec.Body.String()))
		}
	}

	// Два шаблона, которые совпадают на одних и тех же
	// путях без «более специфичного», — паника при
	// регистрации, а не при запросе.
	defer func() { fmt.Println("паника:", recover() != nil) }()
	mux.HandleFunc("GET /{kind}/new", func(http.ResponseWriter, *http.Request) {})
}
