// JSON API на `net/http` без фреймворка: декодирование
// тела, проверка ввода и ответы с правильными кодами —
// это несколько маленьких хелперов, которые пишут один раз.

package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
)

// Ответ-ошибка в формате RFC 9457 (`application/problem+json`).
type problem struct {
	Type   string `json:"type"`
	Title  string `json:"title"`
	Status int    `json:"status"`
	Detail string `json:"detail,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeProblem(w http.ResponseWriter, status int, detail string) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(problem{"about:blank", http.StatusText(status), status, detail})
}

// decodeJSON возвращает HTTP-статус и текст ошибки: тип
// тела, лимит размера, строгий разбор, ровно один объект.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) (int, error) {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		return http.StatusUnsupportedMediaType, errors.New("нужен application/json")
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return http.StatusRequestEntityTooLarge, err
		}
		return http.StatusBadRequest, err
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return http.StatusBadRequest, errors.New("лишние данные после объекта")
	}
	return 0, nil
}

type task struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
	Done  bool   `json:"done"` // без omitempty: false — тоже значение
}

func main() {
	var mu sync.Mutex
	tasks := map[int]task{}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /tasks", func(w http.ResponseWriter, r *http.Request) {
		var in struct {
			Title string `json:"title"`
		}
		if status, err := decodeJSON(w, r, &in); err != nil {
			writeProblem(w, status, err.Error())
			return // без return ответ допишется дальше
		}
		if strings.TrimSpace(in.Title) == "" {
			writeProblem(w, http.StatusUnprocessableEntity, "title пустой")
			return
		}
		mu.Lock()
		t := task{ID: len(tasks) + 1, Title: in.Title}
		tasks[t.ID] = t
		mu.Unlock()
		w.Header().Set("Location", "/tasks/"+strconv.Itoa(t.ID))
		writeJSON(w, http.StatusCreated, t)
	})
	mux.HandleFunc("GET /tasks/{id}", func(w http.ResponseWriter, r *http.Request) {
		id, _ := strconv.Atoi(r.PathValue("id"))
		mu.Lock()
		t, ok := tasks[id]
		mu.Unlock()
		if !ok {
			writeProblem(w, http.StatusNotFound, "нет задачи "+r.PathValue("id"))
			return
		}
		writeJSON(w, http.StatusOK, t)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	for _, c := range []struct{ method, path, ctype, body string }{
		{"POST", "/tasks", "application/json", `{"title":"купить хлеб"}`},
		{"GET", "/tasks/1", "", ""},
		{"GET", "/tasks/9", "", ""},
		{"POST", "/tasks", "text/plain", `{"title":"x"}`},
		{"POST", "/tasks", "application/json", `{"title":"x","prio":1}`},
		{"POST", "/tasks", "application/json", `{"title":"x"}{"title":"y"}`},
		{"POST", "/tasks", "application/json; charset=utf-8", `{"title":"  "}`},
	} {
		req, _ := http.NewRequest(c.method, srv.URL+c.path, strings.NewReader(c.body))
		req.Header.Set("Content-Type", c.ctype)
		resp, err := srv.Client().Do(req)
		if err != nil {
			panic(err)
		}
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		fmt.Printf("%s %s → %d %s", c.method, c.path, resp.StatusCode, resp.Header.Get("Content-Type"))
		if loc := resp.Header.Get("Location"); loc != "" {
			fmt.Print(" Location: ", loc)
		}
		fmt.Printf("\n  %s", body)
	}
}
