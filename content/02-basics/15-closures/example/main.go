// Замыкание — функция, которая использует переменные
// из окружающей области видимости. Захватывается сама
// переменная, а не копия её значения.

package main

import "fmt"

// `counter` возвращает функцию, которая помнит `n`
// между вызовами. `n` живёт, пока жива функция.
func counter() func() int {
	n := 0
	return func() int {
		n++
		return n
	}
}

// Генератор Фибоначчи: состояние — две захваченные
// переменные, без отдельного класса.
func fib() func() int {
	a, b := 0, 1
	return func() int {
		r := a
		a, b = b, a+b
		return r
	}
}

func main() {

	// Каждый вызов `counter` создаёт новую `n` —
	// счётчики независимы.
	c1, c2 := counter(), counter()
	fmt.Println(c1(), c1(), c1(), c2())

	f := fib()
	for range 8 {
		fmt.Print(f(), " ")
	}
	fmt.Println()

	// Замыкание видит текущее значение переменной,
	// а не то, что было при создании.
	x := 1
	show := func() int { return x }
	x = 2
	fmt.Println("x:", show())

	// И может менять её: запись видна снаружи.
	total := 0
	add := func(v int) { total += v }
	add(10)
	add(5)
	fmt.Println("total:", total)

	// С Go 1.22 у каждой итерации `for` своя переменная
	// цикла, поэтому каждое замыкание помнит своё `i`.
	var fs []func() int
	for i := 0; i < 3; i++ {
		fs = append(fs, func() int { return i * 10 })
	}
	for _, g := range fs {
		fmt.Print(g(), " ")
	}
	fmt.Println()

	// Но переменная, объявленная до цикла, — одна на всех.
	var gs []func() int
	shared := 0
	for i := range 3 {
		shared = i
		gs = append(gs, func() int { return shared })
	}
	for _, g := range gs {
		fmt.Print(g(), " ")
	}
	fmt.Println()

	// Замыкание с параметром фиксирует значение при вызове:
	// «фабрика» множителей.
	mul := func(k int) func(int) int {
		return func(v int) int { return v * k }
	}
	double, triple := mul(2), mul(3)
	fmt.Println(double(7), triple(7))
}
