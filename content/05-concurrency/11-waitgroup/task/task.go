package task

import (
	"sync"
	"time"
)

// WaitTimeout ждёт, пока счётчик wg дойдёт до нуля, но не дольше d.
// Возвращает true, если дождалась, и false по таймауту.
func WaitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	// TODO: реализуйте функцию
	return false
}

// RunAll запускает каждую задачу в своей горутине и ждёт их не
// дольше d. Если все успели — ошибки по индексам и true, иначе nil и false.
func RunAll(d time.Duration, tasks ...func() error) ([]error, bool) {
	// TODO: реализуйте функцию
	return nil, false
}
