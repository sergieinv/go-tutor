package world

import (
	"context"
	"time"
)

// maxCatchUp — насколько цикл готов отстать от расписания, догоняя
// пропущенные тики подряд. Дольше — долг списывается: мир «замедляется»,
// а не крутит десятки тиков подряд, пытаясь вернуть прошлое (иначе после
// паузы GC или подвисания машины игроки увидят рывок).
const maxCatchUp = 5 * TickInterval

// schedule — время следующего тика при фиксированном шаге. next — когда
// тик должен был начаться по расписанию, now — сейчас. Если отставание
// больше maxCatchUp, расписание сдвигается на now и возвращается число
// списанных тиков.
func schedule(next, now time.Time) (time.Time, int) {
	lag := now.Sub(next)
	if lag <= maxCatchUp {
		return next, 0 // вовремя или небольшое отставание: догоним подряд
	}
	return now, int(lag / TickInterval)
}

// Run крутит игровой цикл с фиксированным шагом 50 мс до отмены ctx, затем
// отключает игроков и возвращает финальный снапшот. Вызывается один раз;
// горутина Run и есть владелец мира.
func (w *World) Run(ctx context.Context, shutdownText string) *Snapshot {
	defer close(w.stopped)

	timer := time.NewTimer(time.Hour)
	defer timer.Stop()
	next := w.now()
	for {
		if ctx.Err() != nil {
			break
		}
		start := w.now()
		w.measureTPS(start)
		w.Step()
		end := w.now()
		if d := end.Sub(start); d > TickInterval {
			// Тик дольше бюджета. Предупреждаем не чаще раза в 5 с, чтобы
			// перегрузка не превратилась ещё и в шторм журнала.
			if end.Sub(w.lastWarn) > 5*time.Second {
				w.log.Warn("tick overran", "tick", w.tick, "duration", d, "budget", TickInterval, "players", len(w.list))
				w.lastWarn = end
			}
		}

		next = next.Add(TickInterval)
		var skipped int
		next, skipped = schedule(next, end)
		if skipped > 0 {
			w.metrics.TicksSkipped(skipped)
			w.log.Warn("can't keep up, skipping ticks", "skipped", skipped, "tick", w.tick)
		}
		if wait := next.Sub(end); wait > 0 {
			timer.Reset(wait)
			select {
			case <-timer.C:
			case <-ctx.Done():
			}
		}
	}
	snap := w.Shutdown(shutdownText)
	w.log.Info("world stopped", "tick", w.tick)
	return snap
}

// measureTPS — фактическая частота по времени начала последних 20 тиков.
func (w *World) measureTPS(start time.Time) {
	n := len(w.tickStarts)
	w.tickStarts[w.tickStartsN%n] = start
	w.tickStartsN++
	if w.tickStartsN < n {
		return
	}
	oldest := w.tickStarts[w.tickStartsN%n]
	if span := start.Sub(oldest); span > 0 {
		w.tps = min(float64(n-1)/span.Seconds(), TickRate)
		w.metrics.TPS(w.tps)
	}
}
