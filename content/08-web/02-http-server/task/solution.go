package task

import (
	"net/http"
	"strconv"
	"sync"
)

// Counter — HTTP-счётчик. Нулевое значение готово к работе.
type Counter struct {
	mu sync.Mutex
	n  int
}

// ServeHTTP обрабатывает GET/HEAD, POST, DELETE.
func (c *Counter) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		c.mu.Lock()
		n := c.n
		c.mu.Unlock()
		writeValue(w, n)
	case http.MethodPost:
		by := 1
		if r.URL.Query().Has("by") {
			v, err := strconv.Atoi(r.URL.Query().Get("by"))
			if err != nil || v < 1 || v > 1000 {
				http.Error(w, "invalid by", http.StatusBadRequest)
				return
			}
			by = v
		}
		c.mu.Lock()
		c.n += by
		n := c.n
		c.mu.Unlock()
		writeValue(w, n)
	case http.MethodDelete:
		c.mu.Lock()
		c.n = 0
		c.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	default:
		w.Header().Set("Allow", "GET, HEAD, POST, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func writeValue(w http.ResponseWriter, n int) {
	// Заголовки — до первой записи тела.
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Write([]byte(strconv.Itoa(n) + "\n"))
}
