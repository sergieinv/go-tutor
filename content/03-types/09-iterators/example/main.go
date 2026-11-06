// Итератор — функция, которая вызывает `yield` для
// каждого элемента. `for range` умеет обходить такие
// функции с Go 1.23.

package main

import (
	"fmt"
	"iter"
	"maps"
	"slices"
	"strings"
)

// `iter.Seq[int]` — это `func(yield func(int) bool)`.
// Если `yield` вернул `false`, цикл у потребителя
// закончился (`break`, `return`) — нужно выйти.
func Countdown(from int) iter.Seq[int] {
	return func(yield func(int) bool) {
		for i := from; i >= 0; i-- {
			if !yield(i) {
				fmt.Println("  остановлен на", i)
				return
			}
		}
	}
}

// `iter.Seq2` отдаёт пары — как `range` по map.
func Enumerate[T any](s []T) iter.Seq2[int, T] {
	return func(yield func(int, T) bool) {
		for i, v := range s {
			if !yield(i, v) {
				return
			}
		}
	}
}

// Адаптер принимает итератор и возвращает итератор.
// Вычисления ленивые: ничего не происходит до `range`.
func Map[T, U any](seq iter.Seq[T], f func(T) U) iter.Seq[U] {
	return func(yield func(U) bool) {
		for v := range seq {
			if !yield(f(v)) {
				return
			}
		}
	}
}

func main() {
	for v := range Countdown(3) {
		fmt.Print(v, " ")
	}
	fmt.Println()

	// `break` превращается в `yield(...) == false`.
	for v := range Countdown(10) {
		if v == 8 {
			break
		}
		fmt.Println(v)
	}

	for i, s := range Enumerate([]string{"a", "b"}) {
		fmt.Println(i, s)
	}

	// Итераторы стандартной библиотеки. Порядок обхода
	// map случаен — `slices.Sorted` собирает ключи
	// в отсортированный срез.
	ages := map[string]int{"bob": 41, "ann": 35, "eve": 29}
	fmt.Println(slices.Sorted(maps.Keys(ages)))

	// `slices.Values` — итератор по срезу, `slices.Collect`
	// собирает итератор в срез.
	upper := slices.Collect(Map(slices.Values([]string{"go", "c#"}), strings.ToUpper))
	fmt.Println(upper)

	// `iter.Pull` превращает push-итератор в pull:
	// `next` достаёт по одному, `stop` освобождает ресурсы.
	next, stop := iter.Pull(Countdown(2))
	defer stop()
	a, ok1 := next()
	b, ok2 := next()
	fmt.Println(a, ok1, b, ok2)
}
