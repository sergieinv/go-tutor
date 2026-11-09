// Игровой цикл из двух частей. Мир (`World`) принадлежит одной
// горутине: соединения не трогают его состояние, а присылают
// команды в ограниченный канал. Планировщик (`Loop`) решает,
// когда выполнять тик: 20 раз в секунду по сетке 50 мс,
// с догонянием после тяжёлого тика и пропуском, если отставание
// слишком велико. Время внедрено через интерфейс `Clock` —
// тесты в `main_test.go` гоняют цикл на фейковых часах.
package main

import (
	"fmt"
	"slices"
	"time"
)

type Vec struct{ X, Z float64 }

type Player struct {
	Pos  Vec
	Want Vec // куда игрок хочет сдвинуться за этот тик
}

// Всё, что соединения хотят от мира, — команда. Метод `apply`
// вызывается только из горутины мира, поэтому мьютекс не нужен.
type Command interface{ apply(w *World) }

type Join struct{ Name string }

type Move struct {
	Name   string
	DX, DZ float64
}

// Запрос с ответом: мир отвечает в канал, который принёс запрос.
// У канала должен быть буфер 1 — мир не ждёт того, кто спросил.
type Who struct{ Reply chan<- []string }

func (c Join) apply(w *World) { w.players[c.Name] = &Player{} }

func (c Move) apply(w *World) {
	if p, ok := w.players[c.Name]; ok {
		p.Want = Vec{c.DX, c.DZ} // последняя команда за тик побеждает
	}
}

func (c Who) apply(w *World) {
	names := make([]string, 0, len(w.players))
	for n := range w.players {
		names = append(names, n)
	}
	slices.Sort(names)
	c.Reply <- names
}

// Мир и его вход. `cmds` — ограниченный канал: соединение
// не может завалить мир командами.
type World struct {
	Tick    uint64
	players map[string]*Player
	cmds    chan Command
}

func NewWorld(queue int) *World {
	return &World{players: map[string]*Player{}, cmds: make(chan Command, queue)}
}

// `Submit` вызывают горутины чтения соединений. Он не ждёт:
// очередь полна — команда отвергнута, и соединение решает,
// что делать с клиентом, который шлёт быстрее, чем мир читает.
func (w *World) Submit(c Command) bool {
	select {
	case w.cmds <- c:
		return true
	default:
		return false
	}
}

const maxSpeed = 1.0 // блоков за тик

// Один тик в три фазы: собрать ввод, обновить мир, разослать
// изменения. Ввод вычерпывается без ожидания и не больше
// `maxCmds` за тик — иначе поток команд растянул бы тик.
func (w *World) Step(maxCmds int, publish func(tick uint64, events []string)) {
	w.Tick++
drain:
	for range maxCmds {
		select {
		case c := <-w.cmds:
			c.apply(w)
		default:
			break drain
		}
	}

	var events []string
	for name, p := range w.players {
		if p.Want == (Vec{}) {
			continue
		}
		p.Pos.X += clamp(p.Want.X)
		p.Pos.Z += clamp(p.Want.Z)
		p.Want = Vec{}
		events = append(events, fmt.Sprintf("%s → (%.1f, %.1f)", name, p.Pos.X, p.Pos.Z))
	}
	slices.Sort(events) // порядок обхода map случаен
	publish(w.Tick, events)
}

func clamp(v float64) float64 { return max(-maxSpeed, min(maxSpeed, v)) }

// Часы — интерфейс: в сервере `time.Now`/`time.Sleep`,
// в тестах — фейк, который двигают вручную.
type Clock interface {
	Now() time.Duration // монотонное время от старта
	Sleep(d time.Duration)
}

type realClock struct{ start time.Time }

func (c realClock) Now() time.Duration    { return time.Since(c.start) }
func (c realClock) Sleep(d time.Duration) { time.Sleep(d) }

// Планировщик с фиксированным шагом. `next` — запланированное
// время следующего тика; тики стоят на сетке start + k·Interval.
type Loop struct {
	Interval   time.Duration
	MaxCatchUp int // больше тиков подряд без сна не выполнять
	Skipped    int // сколько тиков отброшено при перегрузке
	clock      Clock
	next       time.Duration
}

func NewLoop(c Clock, interval time.Duration, maxCatchUp int) *Loop {
	return &Loop{Interval: interval, MaxCatchUp: maxCatchUp, clock: c, next: c.Now()}
}

// Одна итерация: сколько тиков уже наступило по сетке — столько
// и выполнить (догнать), но не больше `MaxCatchUp`; остаток
// отбросить, как Minecraft при «Can't keep up!». Потом спать
// до следующего тика, если он ещё не наступил.
func (l *Loop) Step(tick func()) int {
	due := 0
	if now := l.clock.Now(); now >= l.next {
		due = int((now-l.next)/l.Interval) + 1
	}
	if due > l.MaxCatchUp {
		l.Skipped += due - l.MaxCatchUp
		l.next += time.Duration(due-l.MaxCatchUp) * l.Interval
		due = l.MaxCatchUp
	}
	for range due {
		tick()
		l.next += l.Interval
	}
	if d := l.next - l.clock.Now(); d > 0 {
		l.clock.Sleep(d)
	}
	return due
}

// В режиме тестов пример запускается через `go test`. В сервере
// горутина мира сама крутит цикл на настоящих часах, а соединения
// общаются с ней только через `Submit`.
func main() {
	w := NewWorld(1024)
	loop := NewLoop(realClock{time.Now()}, 50*time.Millisecond, 3)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for w.Tick < 3 {
			loop.Step(func() { w.Step(64, func(uint64, []string) {}) })
		}
	}()
	w.Submit(Join{"alice"})
	<-done
}
