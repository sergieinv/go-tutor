// Пример собирается с `-gcflags="-m=2 -d=ssa/check_bce/debug=1"`:
// под выводом программы песочница показывает, что решил
// компилятор. `can inline f with cost N` — функция уложилась
// в бюджет встраивания, `cannot inline f: …` — почему нет,
// `inlining call to f` — вызов заменён телом, `Found IsInBounds` —
// в этой строке осталась проверка границ. Сопоставляйте номера
// строк диагностики с кодом ниже.
package main

import "fmt"

// **Маленькая функция встраивается.** Её тело дешевле бюджета
// (80 «узлов»), и в `main` вызова не будет — только сложение.
// Поэтому геттеры и крошечные хелперы в Go ничего не стоят.
func add(a, b int) int { return a + b }

// `//go:noinline` запрещает встраивание — так делают в бенчмарках
// и тестах компилятора: `cannot inline addNoInline: marked
// go:noinline`.
//
//go:noinline
func addNoInline(a, b int) int { return a + b }

// **Встраивание в середине цепочки** (mid-stack inlining):
// `scale` вызывает `add`, и в `main` встраиваются обе.
func scale(x int) int { return add(x, x) * 3 }

// **Вызов дорог.** Каждый вызов функции, которую нельзя встроить,
// добавляет к стоимости 57: два таких вызова не помещаются в 80,
// и `twice` уже не встраивается — `function too complex`.
func twice(x int) int { return addNoInline(x, x) + addNoInline(x, 1) }

// **Четыре проверки.** Компилятор проверяет каждое `b[i]`
// отдельно: `b[0]` в границах ещё не значит, что `b[3]` тоже.
//
//go:noinline
func u32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// **Подсказка `_ = b[3]`.** Одна проверка в начале: если она
// прошла, длина не меньше 4, и остальные индексы доказаны.
// Так написан `binary.LittleEndian.Uint32`.
//
//go:noinline
func u32Hint(b []byte) uint32 {
	_ = b[3]
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// **Цикл до `len(s)`** — проверок нет: компилятор знает,
// что `0 <= i < len(s)`. То же для `for i := range s`.
//
//go:noinline
func sum(s []int) int {
	t := 0
	for i := 0; i < len(s); i++ {
		t += s[i]
	}
	return t
}

// **Цикл до чужой границы** — проверка на каждой итерации:
// `n` может оказаться больше `len(s)`. Решение — сократить срез
// до `n` заранее (`s = s[:n]`): одна проверка вне цикла.
//
//go:noinline
func sumFirst(s []int, n int) int {
	t := 0
	for i := 0; i < n; i++ {
		t += s[i]
	}
	return t
}

//go:noinline
func sumFirstFast(s []int, n int) int {
	s = s[:n] // одна проверка здесь
	t := 0
	for i := range s {
		t += s[i]
	}
	return t
}

func main() {
	b := []byte{0x78, 0x56, 0x34, 0x12}
	s := []int{1, 2, 3, 4, 5}
	r := [...]int{add(2, 3), scale(7), twice(4), sum(s), sumFirst(s, 3), sumFirstFast(s, 3)}
	fmt.Printf("%v %v\n", r, u32(b) == u32Hint(b))
}
