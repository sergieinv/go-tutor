// Параметры типов пишутся в квадратных скобках.
// Ограничение каждого параметра — интерфейс.

package main

import (
	"fmt"
	"strconv"
	"strings"
)

// Обобщённая функция с двумя параметрами типов.
// `any` — любой тип.
func Map[T, U any](s []T, f func(T) U) []U {
	out := make([]U, 0, len(s))
	for _, v := range s {
		out = append(out, f(v))
	}
	return out
}

func Filter[T any](s []T, keep func(T) bool) []T {
	var out []T
	for _, v := range s {
		if keep(v) {
			out = append(out, v)
		}
	}
	return out
}

// `comparable` разрешает `==` и `!=`.
func Index[T comparable](s []T, v T) int {
	for i := range s {
		if s[i] == v {
			return i
		}
	}
	return -1
}

// Обобщённый тип. Методы используют параметр типа,
// но объявить свой параметр в Go 1.25 метод не может.
type Stack[T any] struct {
	items []T
}

func (s *Stack[T]) Push(v T) { s.items = append(s.items, v) }

// `var zero T` — нулевое значение любого `T`,
// аналог `default(T)` в C#.
func (s *Stack[T]) Pop() (T, bool) {
	var zero T
	if len(s.items) == 0 {
		return zero, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}

func main() {
	nums := []int{1, 2, 3, 4}

	// Типы выводятся из аргументов: T = int, U = string.
	strs := Map(nums, strconv.Itoa)
	fmt.Println(strings.Join(strs, "+"))

	even := Filter(nums, func(n int) bool { return n%2 == 0 })
	fmt.Println(even)

	// Явное указание аргументов типа тоже допустимо.
	fmt.Println(Index[string](strs, "3"), Index(nums, 10))

	// Для обобщённого типа аргумент указывают явно.
	var s Stack[string]
	s.Push("a")
	s.Push("b")
	v, ok := s.Pop()
	fmt.Println(v, ok)

	// Каждое инстанцирование — отдельный тип.
	fmt.Printf("%T %T\n", s, Stack[int]{})

	// На пустом стеке `Pop` возвращает нулевое значение T.
	var empty Stack[float64]
	fmt.Println(empty.Pop())
}
