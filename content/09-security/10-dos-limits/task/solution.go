package task

import (
	"compress/gzip"
	"net/http"
	"time"
)

// Limits — лимиты сервера. В тестах все поля положительны.
type Limits struct {
	HeaderTimeout   time.Duration // ReadHeaderTimeout
	ReadTimeout     time.Duration
	WriteTimeout    time.Duration
	IdleTimeout     time.Duration
	MaxHeaderBytes  int
	MaxBodyBytes    int64 // тело запроса как пришло по сети
	MaxDecompressed int64 // тело после распаковки gzip
	MaxInFlight     int   // одновременных запросов в next
}

// Protect оборачивает next лимитами на тело, распаковку и параллелизм.
func Protect(next http.Handler, l Limits) http.Handler {
	slots := make(chan struct{}, l.MaxInFlight)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case slots <- struct{}{}:
			defer func() { <-slots }()
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "server busy", http.StatusServiceUnavailable)
			return
		}

		r.Body = http.MaxBytesReader(w, r.Body, l.MaxBodyBytes)
		switch r.Header.Get("Content-Encoding") {
		case "":
		case "gzip":
			zr, err := gzip.NewReader(r.Body)
			if err != nil {
				http.Error(w, "bad gzip", http.StatusBadRequest)
				return
			}
			defer zr.Close()
			// MaxBytesReader поверх распаковщика: тот же тип ошибки
			// *http.MaxBytesError, ровно max байт — ещё не ошибка.
			r.Body = http.MaxBytesReader(w, zr, l.MaxDecompressed)
			r.Header.Del("Content-Encoding")
			r.ContentLength = -1
		default:
			http.Error(w, "unsupported encoding", http.StatusUnsupportedMediaType)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// NewServer возвращает сервер с таймаутами и лимитами из l.
func NewServer(h http.Handler, l Limits) *http.Server {
	return &http.Server{
		Handler:           Protect(h, l),
		ReadHeaderTimeout: l.HeaderTimeout,
		ReadTimeout:       l.ReadTimeout,
		WriteTimeout:      l.WriteTimeout,
		IdleTimeout:       l.IdleTimeout,
		MaxHeaderBytes:    l.MaxHeaderBytes,
	}
}
