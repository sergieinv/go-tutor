// Функции объявляются словом `func`. Тип пишется после
// имени, функции живут на уровне пакета, а не в классе.

package main

import (
	"fmt"
	"strings"
)

// Параметры одного типа подряд можно объединить:
// `a, b int` — то же, что `a int, b int`.
func add(a, b int) int {
	return a + b
}

// Без результата тип не пишут — это аналог `void`.
func greet(name string, times int) {
	fmt.Println(strings.Repeat("hi "+name+" ", times))
}

// Тип функции можно назвать, как делегат в C#.
type op func(int, int) int

// Функция принимает другую функцию параметром.
func apply(f op, a, b int) int {
	return f(a, b)
}

// И возвращает функцию результатом.
func pick(name string) op {
	switch name {
	case "add":
		return add
	case "mul":
		// Анонимная функция — литерал `func` без имени.
		return func(a, b int) int { return a * b }
	}
	return nil
}

func main() {
	fmt.Println(add(2, 3))
	greet("Go", 2)

	// Функция — значение: её можно присвоить переменной.
	// Тип `f` — `func(int, int) int`.
	f := add
	fmt.Printf("%T %d\n", f, f(10, 20))

	fmt.Println(apply(add, 4, 5), apply(pick("mul"), 4, 5))

	// Анонимную функцию можно объявить и сразу вызвать.
	sq := func(x int) int { return x * x }(7)
	fmt.Println(sq)

	// Нулевое значение функционального типа — `nil`.
	// Вызов `nil`-функции — паника, поэтому проверяем.
	if g := pick("div"); g == nil {
		fmt.Println("no such op")
	}

	// Функции стандартной библиотеки тоже принимают
	// функции: `strings.Map` применяет её к каждой руне.
	rot := strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' {
			return 'a' + (r-'a'+13)%26
		}
		return r
	}, "hello, go")
	fmt.Println(rot)

	// Перегрузки нет: вторая `add(a, b, c int)` в этом
	// пакете не скомпилировалась бы. Даём другое имя.
	fmt.Println(add3(1, 2, 3))
}

func add3(a, b, c int) int {
	return add(add(a, b), c)
}
