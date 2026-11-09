package task

import (
	"context"
	"time"
)

// Clock — внедрённые часы: монотонное время от старта и сон.
type Clock interface {
	Now() time.Duration
	Sleep(d time.Duration)
}

// Config — настройки цикла (см. «Задание»).
type Config struct {
	Interval   time.Duration // длительность тика; <= 0 — 50 мс
	MaxCatchUp int           // сколько тиков подряд без сна максимум; < 1 — 1
	Window     int           // по скольким последним тикам считать MSPT; < 1 — 20
}

// Stats — счётчики цикла.
type Stats struct {
	Ticks   uint64        // выполнено тиков всего
	Skipped uint64        // отброшено тиков всего
	MSPT    time.Duration // среднее время тика по последним Window тикам
	MaxMSPT time.Duration // максимальное время тика по тем же тикам
}

type Loop struct {
	clock Clock
	cfg   Config
	tick  func(n uint64)
	next  time.Duration

	ticks   uint64
	skipped uint64
	window  []time.Duration // кольцо длительностей последних тиков
}

func NewLoop(clock Clock, cfg Config, tick func(n uint64)) *Loop {
	if cfg.Interval <= 0 {
		cfg.Interval = 50 * time.Millisecond
	}
	if cfg.MaxCatchUp < 1 {
		cfg.MaxCatchUp = 1
	}
	if cfg.Window < 1 {
		cfg.Window = 20
	}
	return &Loop{
		clock:  clock,
		cfg:    cfg,
		tick:   tick,
		next:   clock.Now(),
		window: make([]time.Duration, 0, cfg.Window),
	}
}

func (l *Loop) Step() int {
	due := 0
	if now := l.clock.Now(); now >= l.next {
		due = int((now-l.next)/l.cfg.Interval) + 1
	}
	if due > l.cfg.MaxCatchUp {
		extra := due - l.cfg.MaxCatchUp
		l.skipped += uint64(extra)
		l.next += time.Duration(extra) * l.cfg.Interval
		due = l.cfg.MaxCatchUp
	}
	for range due {
		start := l.clock.Now()
		l.tick(l.ticks + 1)
		l.record(l.clock.Now() - start)
		l.ticks++
		l.next += l.cfg.Interval
	}
	if d := l.next - l.clock.Now(); d > 0 {
		l.clock.Sleep(d)
	}
	return due
}

// record кладёт длительность тика в кольцо: пока оно не заполнено —
// дописывает, потом перезаписывает самую старую.
func (l *Loop) record(d time.Duration) {
	if len(l.window) < l.cfg.Window {
		l.window = append(l.window, d)
		return
	}
	l.window[l.ticks%uint64(l.cfg.Window)] = d
}

func (l *Loop) Run(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		l.Step()
	}
}

func (l *Loop) Stats() Stats {
	s := Stats{Ticks: l.ticks, Skipped: l.skipped}
	if len(l.window) == 0 {
		return s
	}
	var sum time.Duration
	for _, d := range l.window {
		sum += d
		s.MaxMSPT = max(s.MaxMSPT, d)
	}
	s.MSPT = sum / time.Duration(len(l.window))
	return s
}
