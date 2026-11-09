package task

import (
	"net/http"
	"time"
)

// Capture сохраняет снимок трассы после медленного запроса: не чаще
// раза в Cooldown и не больше одного снимка одновременно.
type Capture struct {
	Threshold time.Duration
	Cooldown  time.Duration
	Now       func() time.Time
	Snapshot  func(reason string) error

	// TODO: состояние — мьютекс, идёт ли снимок, момент последней попытки, счётчик
}

// Middleware меряет время запроса и при необходимости делает снимок.
func (c *Capture) Middleware(next http.Handler) http.Handler {
	// TODO: два вызова Now вокруг next.ServeHTTP, проверка порога, попытка снимка
	return next
}

// Captured возвращает число удачных снимков.
func (c *Capture) Captured() int64 {
	// TODO
	return 0
}
