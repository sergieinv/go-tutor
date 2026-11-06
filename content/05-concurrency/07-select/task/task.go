package task

import (
	"errors"
	"time"
)

var (
	ErrTimeout  = errors.New("timeout")
	ErrNoResult = errors.New("all sources closed")
)

// Race возвращает первое значение из a или b.
// Закрытый канал выбывает из гонки; если закрыты оба —
// ErrNoResult. Если за timeout ничего не пришло — ErrTimeout.
func Race(a, b <-chan string, timeout time.Duration) (string, error) {
	// TODO: реализуйте функцию
	return "", nil
}
