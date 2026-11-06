// Variadic-функция принимает любое число аргументов
// последнего параметра. Внутри это обычный срез.

package main

import (
	"fmt"
	"strings"
)

// `nums ...int` — внутри функции это `[]int`.
func sum(nums ...int) int {
	total := 0
	for _, n := range nums {
		total += n
	}
	return total
}

// Variadic-параметр может быть только последним.
func join(sep string, parts ...string) string {
	return strings.Join(parts, sep)
}

// Функция пишет в срез — и это видно снаружи,
// если вызвали с разворотом `s...`.
func double(nums ...int) {
	for i := range nums {
		nums[i] *= 2
	}
}

// `...any` принимает значения любых типов — так
// устроены `fmt.Println` и `fmt.Printf`.
func logf(format string, args ...any) {
	fmt.Printf("[log] "+format+"\n", args...)
}

func main() {
	fmt.Println(sum(1, 2, 3))

	// Без аргументов параметр равен `nil` — длина 0,
	// `range` по нему просто не выполняется.
	fmt.Println(sum())

	fmt.Println(join("-", "a", "b", "c"))

	// Готовый срез разворачивают через `s...`.
	// Без `...` вызов не скомпилируется: `[]int` — не `int`.
	s := []int{10, 20, 30}
	fmt.Println(sum(s...))

	// С отдельными аргументами компилятор собирает новый
	// срез: изменения внутри функции снаружи не видны.
	a, b := 1, 2
	double(a, b)
	fmt.Println("args:", a, b)

	// С разворотом функция получает тот же массив,
	// что у вызывающего кода, — и меняет его.
	double(s...)
	fmt.Println("spread:", s)

	// Аргументы передаются дальше тоже через `...`:
	// `logf` разворачивает `args` в вызов `Printf`.
	logf("%s=%d, ok=%v", "x", 42, true)

	// Встроенная `append` тоже variadic: так склеивают
	// два среза.
	more := append([]int{1, 2}, s...)
	fmt.Println(more)
}
