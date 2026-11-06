package task

import "time"

// Limiter — ограничитель частоты на ведре токенов.
type Limiter struct {
	// TODO: поля
}

// NewLimiter создаёт полное ведро на burst токенов,
// которое пополняется на один токен каждые interval.
func NewLimiter(interval time.Duration, burst int) *Limiter {
	// TODO: реализуйте функцию
	return &Limiter{}
}

// Allow забирает токен, если он есть, не дожидаясь.
func (l *Limiter) Allow() bool {
	// TODO
	return false
}

// Wait ждёт токен и возвращает true. После Stop не блокируется:
// забирает оставшийся токен, если он есть, иначе возвращает false.
func (l *Limiter) Wait() bool {
	// TODO
	return false
}

// Stop прекращает пополнение ведра.
func (l *Limiter) Stop() {
	// TODO
}
