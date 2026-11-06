package task

import "time"

// Limiter — ограничитель частоты на ведре токенов.
type Limiter struct {
	tokens chan struct{}
	done   chan struct{}
}

// NewLimiter создаёт полное ведро на burst токенов,
// которое пополняется на один токен каждые interval.
func NewLimiter(interval time.Duration, burst int) *Limiter {
	burst = max(burst, 1)
	l := &Limiter{
		tokens: make(chan struct{}, burst),
		done:   make(chan struct{}),
	}
	for range burst {
		l.tokens <- struct{}{}
	}
	ticker := time.NewTicker(interval)
	go func() {
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				// Ведро полное — токен выбрасываем, а не ждём:
				// иначе горутина не увидела бы Stop.
				select {
				case l.tokens <- struct{}{}:
				default:
				}
			case <-l.done:
				return
			}
		}
	}()
	return l
}

// Allow забирает токен, если он есть, не дожидаясь.
func (l *Limiter) Allow() bool {
	select {
	case <-l.tokens:
		return true
	default:
		return false
	}
}

// Wait ждёт токен и возвращает true. После Stop не блокируется:
// забирает оставшийся токен, если он есть, иначе возвращает false.
func (l *Limiter) Wait() bool {
	select {
	case <-l.tokens:
		return true
	case <-l.done:
		// Остановлен: новых токенов не будет, ждать нечего.
		// Остаток ведра ещё можно забрать.
		return l.Allow()
	}
}

// Stop прекращает пополнение ведра.
func (l *Limiter) Stop() {
	close(l.done)
}
