// Где окажется значение — на стеке или в куче, — решает
// компилятор, а не `new` или `&`. Урок собирается с
// `-gcflags=-m`: под выводом программы песочница покажет,
// что компилятор решил про каждую строку. А сколько
// выделений в куче на самом деле, считает
// `testing.AllocsPerRun` — он работает и вне тестов.

package main

import (
	"fmt"
	"testing"
)

type point struct{ x, y int }

// Глобальные «стоки» не дают компилятору выбросить
// результат как неиспользуемый.
var (
	sinkP   *point
	sinkAny any
	sinkN   int
)

// Возврат по значению: `point` копируется в кадр
// вызывающего, куча не нужна.
func byValue(x, y int) point { return point{x, y} }

// Возврат указателя на локальную переменную: `p`
// переживает функцию — «moved to heap: p».
// `//go:noinline` запрещает встраивание: иначе компилятор
// решал бы заново в месте вызова.
//
//go:noinline
func byPointer(x, y int) *point {
	p := point{x, y}
	return &p
}

// `&point{}` — ещё не куча: указатель не покидает
// функцию, и объект живёт в её кадре.
//
//go:noinline
func local() int {
	p := &point{3, 4}
	return p.x*p.x + p.y*p.y
}

// Буфер переменного размера, который не убегает: с Go 1.25
// компилятор держит на стеке 32 байта под него, и `make`
// идёт в кучу, только если не поместился.
//
//go:noinline
func scratch(n int) int {
	buf := make([]byte, n)
	return len(buf)
}

// Упаковка в интерфейс копирует значение в кучу —
// компилятор не знает, сколько проживёт `any`. Числа
// 0..255 рантайм берёт из готовой таблицы, а указатель
// кладётся в интерфейс как есть, без копии.
//
//go:noinline
func boxInt(v int) any { return v }

//go:noinline
func boxPtr(p *point) any { return p }

// `show` печатает среднее число выделений за вызов `f`.
func show(name string, f func()) {
	fmt.Printf("%-10s %v\n", name, testing.AllocsPerRun(100, f))
}

func main() {
	show("byValue", func() { sinkN = byValue(1, 2).x })
	show("byPointer", func() { sinkP = byPointer(1, 2) })
	show("local", func() { sinkN = local() })

	show("any(7)", func() { sinkAny = boxInt(7) })
	show("any(1000)", func() { sinkAny = boxInt(1000) })
	p := &point{}
	show("any(ptr)", func() { sinkAny = boxPtr(p) })

	show("make(32)", func() { sinkN = scratch(32) })
	show("make(33)", func() { sinkN = scratch(33) })
}
