// Живой визуализатор: игровой цикл на настоящих часах, 20 TPS.
// Каждый тик — задача runtime/trace (строка схемы), фазы тика —
// регионы «ввод», «обновление», «рассылка». Тик 3 тяжёлый: 130 мс
// вместо нескольких. Видно, как следующие тики идут подряд без
// паузы — цикл догоняет сетку, — а потом снова через 50 мс.
//
// Попробуйте: сделайте тяжёлый тик длиннее (300 мс) и посмотрите,
// как сработает maxCatchUp; или замените цикл на time.Ticker —
// тики после тяжёлого пропадут.
package main

import (
	"context"
	"fmt"
	"runtime/trace"
	"time"
)

const (
	interval   = 50 * time.Millisecond
	maxCatchUp = 3
	totalTicks = 9 // схема рисует не больше 10 строк
)

// tick — один тик мира в три фазы. Работа изображена сном.
func tick(n int) {
	ctx, task := trace.NewTask(context.Background(), fmt.Sprintf("тик %d", n))
	defer task.End()
	trace.WithRegion(ctx, "ввод", func() { time.Sleep(time.Millisecond) })
	work := 4 * time.Millisecond
	if n == 3 {
		work = 130 * time.Millisecond
		trace.Log(ctx, "MSPT", "тяжёлый тик: 130 мс при бюджете 50 мс")
	}
	trace.WithRegion(ctx, "обновление", func() { time.Sleep(work) })
	trace.WithRegion(ctx, "рассылка", func() { time.Sleep(time.Millisecond) })
}

func main() {
	start := time.Now()
	now := func() time.Duration { return time.Since(start) }
	next := now()
	ticks, skipped := 0, 0
	for ticks < totalTicks {
		due := 0
		if t := now(); t >= next {
			due = int((t-next)/interval) + 1
		}
		if due > maxCatchUp {
			skipped += due - maxCatchUp
			next += time.Duration(due-maxCatchUp) * interval
			due = maxCatchUp
		}
		for range min(due, totalTicks-ticks) {
			ticks++
			tick(ticks)
			next += interval
		}
		if d := next - now(); d > 0 {
			time.Sleep(d)
		}
	}
	fmt.Println("тиков выполнено:", ticks)
	if skipped > 0 {
		fmt.Println("отброшено:", skipped)
	}
}
