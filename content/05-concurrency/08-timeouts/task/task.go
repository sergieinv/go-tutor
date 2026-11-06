package task

import (
	"errors"
	"time"
)

var ErrTimeout = errors.New("timeout")

// Call выполняет f в отдельной горутине и ждёт результат
// не дольше timeout. По таймауту возвращает нулевое значение
// и ErrTimeout; горутина с f не должна утечь.
func Call[T any](timeout time.Duration, f func() (T, error)) (T, error) {
	// TODO: реализуйте функцию
	var zero T
	return zero, nil
}
