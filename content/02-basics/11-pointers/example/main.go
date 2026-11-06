// Указатель хранит адрес переменной. `&x` берёт адрес,
// `*p` — значение по адресу. Арифметики указателей нет.

package main

import "fmt"

// Аргументы всегда копируются: `incValue` меняет копию.
func incValue(n int) {
	n++
}

// Указатель позволяет изменить переменную вызывающего
// кода — как `ref` в C#, только явно и на вызове.
func incPtr(n *int) {
	*n++
}

type point struct {
	X, Y int
}

// Возвращать адрес локальной переменной безопасно:
// компилятор увидит это и разместит её в куче.
func newPoint(x, y int) *point {
	p := point{X: x, Y: y}
	return &p
}

func main() {

	// `&x` — адрес `x`, тип `*int`. Через `*p` читаем
	// и пишем саму `x`.
	x := 1
	p := &x
	*p = 2
	fmt.Println("x:", x, "*p:", *p, "same:", p == &x)

	incValue(x)
	fmt.Println("incValue:", x)
	incPtr(&x)
	fmt.Println("incPtr:", x)

	// `new(T)` выделяет нулевое значение `T` и возвращает
	// указатель. Нулевое значение самого указателя — `nil`.
	q := new(int)
	var r *int
	fmt.Println("new:", *q, "nil:", r == nil)

	// Структура копируется целиком, как `struct` в C#.
	// Чтобы делить её, передают `*point`.
	a := point{1, 2}
	b := a
	b.X = 100
	c := &a
	c.X = 10 // то же, что (*c).X — разыменование неявное
	fmt.Println("copy:", a, b)

	pt := newPoint(3, 4)
	fmt.Println("newPoint:", *pt, pt.Y)

	// Можно взять адрес элемента среза или поля структуры.
	// Запись через такой указатель меняет сам элемент.
	nums := []int{5, 6, 7}
	second := &nums[1]
	*second *= 10
	y := &a.Y
	*y = -1
	fmt.Println("elem:", nums, "field:", a)

	// Указатели сравнимы: равны, если указывают на одну
	// переменную (или оба `nil`).
	fmt.Println("eq:", &nums[0] == &nums[0], q == new(int))

	// Разыменование `nil` — паника времени выполнения.
	// `defer` с `recover` (модуль 4) перехватывает её,
	// чтобы показать текст ошибки.
	defer func() { fmt.Println("recovered:", recover()) }()
	fmt.Println(*r)
}
