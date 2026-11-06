// `if` в Go — без скобок вокруг условия, но всегда
// с фигурными скобками. Условие обязано быть `bool`.

package main

import (
	"fmt"
	"strconv"
)

func main() {

	// Обычная цепочка `if` / `else if` / `else`.
	// `else` пишется на той же строке, что и `}`.
	t := 23
	if t < 0 {
		fmt.Println("мороз")
	} else if t < 20 {
		fmt.Println("прохладно")
	} else {
		fmt.Println("тепло")
	}

	// Init-выражение выполняется перед условием.
	// `n` и `err` видны в условии, в теле и во всех
	// ветках `else`, но не после `if`.
	for _, s := range []string{"42", "4x2"} {
		if n, err := strconv.Atoi(s); err != nil {
			fmt.Println("ошибка:", err)
		} else {
			fmt.Println("число:", n*2)
		}
	}

	// Тернарного оператора нет. Вместо `a > b ? a : b`
	// пишут обычный `if` с начальным значением.
	a, b := 3, 7
	larger := b
	if a > b {
		larger = a
	}
	fmt.Println("larger:", larger)

	// Для минимума и максимума есть встроенные
	// функции `min` и `max` (Go 1.21+).
	fmt.Println("min/max:", min(a, b), max(a, b, 10))

	// Идиоматичный Go избегает глубокой вложенности:
	// ошибочные случаи обрабатывают сразу и выходят,
	// а основной путь остаётся без отступа.
	fmt.Println(describe("17"), describe("-1"), describe("abc"))
}

func describe(s string) string {
	n, err := strconv.Atoi(s)
	if err != nil {
		return "не число"
	}
	if n < 0 {
		return "отрицательное"
	}
	return "ok " + strconv.Itoa(n)
}
