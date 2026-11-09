package main

import (
	"fmt"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// Горутина мира в тесте: тик по сигналу из `ticks`, изменения
// уходят в `out`. Чтение из `out` — подтверждение, что тик
// закончился, поэтому порядок событий детерминирован.
func startWorld(w *World) (ticks chan<- struct{}, out <-chan []string) {
	tc := make(chan struct{})
	oc := make(chan []string)
	go func() {
		for range tc {
			w.Step(64, func(_ uint64, ev []string) { oc <- ev })
		}
		close(oc)
	}()
	return tc, oc
}

// Три соединения одновременно шлют команды — мир применяет их
// в своей горутине на ближайшем тике. Ни одного мьютекса,
// и `-race` молчит.
func TestWorldOwnsState(t *testing.T) {
	w := NewWorld(16)
	ticks, out := startWorld(w)
	defer close(ticks)

	var wg sync.WaitGroup
	for i, name := range []string{"alice", "bob", "carol"} {
		wg.Go(func() { // горутина чтения соединения
			w.Submit(Join{name})
			w.Submit(Move{name, float64(i), 5}) // 5 — больше максимума
		})
	}
	wg.Wait()
	ticks <- struct{}{}
	for _, e := range <-out {
		t.Log("тик 1:", e)
	}

	// Запрос с ответом: ответ придёт на следующем тике.
	reply := make(chan []string, 1)
	w.Submit(Who{reply})
	ticks <- struct{}{}
	<-out
	t.Log("игроки:", <-reply)
}

// Ограниченный вход: мир не успевает — команда отвергнута
// сразу, горутина соединения не висит.
func TestInputQueueFull(t *testing.T) {
	w := NewWorld(4)
	accepted := 0
	for range 6 {
		if w.Submit(Move{"alice", 1, 0}) {
			accepted++
		}
	}
	t.Logf("принято %d из 6, остальные — отказ", accepted)
	if accepted != 4 {
		t.Fatal(accepted)
	}
}

// Фейковые часы: время идёт только в `Sleep` и в «работе» тика.
type fakeClock struct{ now time.Duration }

func (c *fakeClock) Now() time.Duration    { return c.now }
func (c *fakeClock) Sleep(d time.Duration) { c.now += d }

// Прогон цикла: `cost[n]` — сколько длится тик n (по умолчанию
// 10 мс). Возвращает время начала каждого тика.
func runLoop(maxCatchUp, ticks int, cost map[int]time.Duration) (starts []time.Duration, l *Loop) {
	clk := &fakeClock{}
	l = NewLoop(clk, 50*time.Millisecond, maxCatchUp)
	for len(starts) < ticks {
		l.Step(func() {
			starts = append(starts, clk.now)
			d, ok := cost[len(starts)]
			if !ok {
				d = 10 * time.Millisecond
			}
			clk.now += d
		})
	}
	return starts, l
}

// Тик 3 длится 130 мс — сетка ушла вперёд на два тика.
// Цикл выполняет их подряд без сна и возвращается на сетку.
func TestCatchUp(t *testing.T) {
	starts, l := runLoop(3, 7, map[int]time.Duration{3: 130 * time.Millisecond})
	for i, s := range starts {
		t.Logf("тик %d начался в %v", i+1, s)
	}
	if l.Skipped != 0 || starts[6] != 300*time.Millisecond {
		t.Fatal(starts, l.Skipped)
	}
}

// Пауза 400 мс (сборка мусора, сохранение мира): наступило
// восемь тиков, догоняем не больше трёх, остальные пропускаем —
// время мира отстаёт от настенного.
func TestSkipWhenFarBehind(t *testing.T) {
	starts, l := runLoop(3, 6, map[int]time.Duration{2: 400 * time.Millisecond})
	for i, s := range starts {
		t.Logf("тик %d начался в %v", i+1, s)
	}
	t.Logf("пропущено тиков: %d", l.Skipped)
	if l.Skipped != 5 {
		t.Fatal(l.Skipped)
	}
}

// Для сравнения — `time.Ticker` в пузыре `synctest`, где время
// фейковое. Тик 3 длится 130 мс: в канале тикера место для одного
// значения, лишние тики выброшены — догонять нечего.
func TestTickerDropsTicks(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		tk := time.NewTicker(50 * time.Millisecond)
		defer tk.Stop()
		var got []string
		for n := 1; n <= 6; n++ {
			at := <-tk.C // время, на которое тик был запланирован
			got = append(got, fmt.Sprintf("%v (получен в %v)", at.Sub(start), time.Since(start)))
			if n == 3 {
				time.Sleep(130 * time.Millisecond) // тяжёлый тик
			}
		}
		for i, s := range got {
			t.Logf("тик %d: %s", i+1, s)
		}
	})
}
