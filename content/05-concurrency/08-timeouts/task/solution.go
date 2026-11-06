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
	var zero T
	if timeout <= 0 {
		return zero, ErrTimeout
	}

	type result struct {
		v   T
		err error
	}
	// Буфер на одно значение: после таймаута исполнитель
	// положит результат в буфер и завершится, а не зависнет.
	ch := make(chan result, 1)
	go func() {
		v, err := f()
		ch <- result{v, err}
	}()

	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(timeout):
		return zero, ErrTimeout
	}
}
