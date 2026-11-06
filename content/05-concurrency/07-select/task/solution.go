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
	// Один таймер на весь вызов: time.After внутри цикла
	// перезапускал бы отсчёт на каждой итерации.
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	closedA, closedB := false, false
	for !closedA || !closedB {
		select {
		case v, ok := <-a:
			if ok {
				return v, nil
			}
			a, closedA = nil, true
		case v, ok := <-b:
			if ok {
				return v, nil
			}
			b, closedB = nil, true
		case <-timer.C:
			return "", ErrTimeout
		}
	}
	return "", ErrNoResult
}
