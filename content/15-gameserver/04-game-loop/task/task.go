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
	// TODO: часы, настройки, функция тика, время следующего тика, счётчики
}

func NewLoop(clock Clock, cfg Config, tick func(n uint64)) *Loop {
	// TODO: значения по умолчанию; первый тик — в clock.Now()
	return &Loop{}
}

func (l *Loop) Step() int {
	// TODO: сколько тиков наступило, догнать не больше MaxCatchUp,
	// лишние отбросить, поспать до следующего тика
	return 0
}

func (l *Loop) Run(ctx context.Context) error {
	// TODO
	return nil
}

func (l *Loop) Stats() Stats {
	// TODO
	return Stats{}
}
