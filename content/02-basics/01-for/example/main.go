// В Go одно ключевое слово для циклов — `for`.
// Всё, что в C# делают `for`, `while`, `do-while`
// и `foreach`, здесь записывается через него.

package main

import "fmt"

func main() {

	// Классический цикл: инициализация, условие, шаг.
	// Скобок вокруг заголовка нет, фигурные скобки
	// тела обязательны даже для одной строки.
	for i := 0; i < 3; i++ {
		fmt.Print(i, " ")
	}
	fmt.Println("— классический")

	// Только условие — это и есть `while`.
	n := 1
	for n < 100 {
		n *= 3
	}
	fmt.Println("while:", n)

	// Без условия — бесконечный цикл. Выходят из него
	// через `break` или `return`. Так же пишется
	// аналог `do-while`: проверка в конце тела.
	attempts := 0
	for {
		attempts++
		if attempts == 4 {
			break
		}
	}
	fmt.Println("attempts:", attempts)

	// С Go 1.22 можно обойти числа от 0 до n-1
	// через `range` — без счётчика и условия.
	// `continue` переходит к следующей итерации.
	for i := range 6 {
		if i%2 == 0 {
			continue
		}
		fmt.Print(i, " ")
	}
	fmt.Println("— нечётные")

	// Если индекс не нужен, его можно не писать.
	count := 0
	for range 5 {
		count++
	}
	fmt.Println("count:", count)

	// Метка перед циклом позволяет выйти из внешнего
	// цикла прямо из вложенного: `break outer`.
	// Без метки `break` завершил бы только внутренний.
	grid := [][]int{{1, 2, 3}, {4, -5, 6}, {7, 8, 9}}
outer:
	for r, row := range grid {
		for c, v := range row {
			if v < 0 {
				fmt.Printf("negative at [%d][%d]\n", r, c)
				break outer
			}
		}
	}

	// У каждой итерации своя переменная `i`, поэтому
	// замыкания запоминают разные значения.
	var fns []func() int
	for i := 0; i < 3; i++ {
		fns = append(fns, func() int { return i * 10 })
	}
	for _, f := range fns {
		fmt.Print(f(), " ")
	}
	fmt.Println("— замыкания")
}
