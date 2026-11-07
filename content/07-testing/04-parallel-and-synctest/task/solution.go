package task

import (
	"sync"
	"time"
)

// Debouncer вызывает f после паузы в потоке событий.
type Debouncer struct {
	wait, maxWait time.Duration
	f             func()

	mu      sync.Mutex
	timer   *time.Timer
	first   time.Time // начало текущей серии
	pending bool      // вызов f запланирован
	stopped bool
	gen     int // номер последнего запланированного таймера
}

// NewDebouncer создаёт дебаунсер: f вызывается через wait после
// последнего Trigger, но не позже maxWait после первого (если maxWait > 0).
func NewDebouncer(wait, maxWait time.Duration, f func()) *Debouncer {
	return &Debouncer{wait: wait, maxWait: maxWait, f: f}
}

// Trigger сообщает о событии.
func (d *Debouncer) Trigger() {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopped {
		return
	}
	now := time.Now()
	if !d.pending {
		d.pending = true
		d.first = now
	}
	at := now.Add(d.wait)
	if d.maxWait > 0 {
		if limit := d.first.Add(d.maxWait); limit.Before(at) {
			at = limit
		}
	}
	if d.timer != nil {
		d.timer.Stop()
	}
	// Таймер, сработавший до Stop, но не успевший взять mutex,
	// узнает по номеру, что его уже заменили.
	d.gen++
	gen := d.gen
	d.timer = time.AfterFunc(at.Sub(now), func() { d.fire(gen) })
}

func (d *Debouncer) fire(gen int) {
	d.mu.Lock()
	if gen != d.gen || !d.pending {
		d.mu.Unlock()
		return
	}
	d.pending = false
	d.mu.Unlock()
	d.f() // вне mutex: f может снова вызвать Trigger
}

// Stop отменяет ожидаемый вызов и выключает дебаунсер.
func (d *Debouncer) Stop() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	wasPending := d.pending && !d.stopped
	d.stopped = true
	d.pending = false
	d.gen++
	if d.timer != nil {
		d.timer.Stop()
	}
	return wasPending
}
