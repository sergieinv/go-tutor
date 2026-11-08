package task

import (
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
	// TODO: реализуйте функцию
	return next
}

// NewServer возвращает сервер с таймаутами и лимитами из l.
func NewServer(h http.Handler, l Limits) *http.Server {
	// TODO: реализуйте функцию
	return &http.Server{Handler: h}
}
