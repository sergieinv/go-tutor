package task

import "time"

// Debouncer вызывает f после паузы в потоке событий.
type Debouncer struct {
	// TODO: поля
}

// NewDebouncer создаёт дебаунсер: f вызывается через wait после
// последнего Trigger, но не позже maxWait после первого (если maxWait > 0).
func NewDebouncer(wait, maxWait time.Duration, f func()) *Debouncer {
	// TODO: реализуйте конструктор
	return &Debouncer{}
}

// Trigger сообщает о событии.
func (d *Debouncer) Trigger() {
	// TODO: реализуйте метод
}

// Stop отменяет ожидаемый вызов и выключает дебаунсер.
func (d *Debouncer) Stop() bool {
	// TODO: реализуйте метод
	return false
}
