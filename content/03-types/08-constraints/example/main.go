// Ограничение — интерфейс. Кроме методов, он может
// перечислять допустимые типы — тогда в теле функции
// доступны их общие операторы.

package main

import (
	"cmp"
	"fmt"
	"slices"
	"strings"
	"time"
)

// Объединение типов. `~int64` — любой тип с базовым
// типом `int64`, например `time.Duration`.
type Number interface {
	~int | ~int64 | ~float64
}

// `+` есть у всех типов набора — его можно применять.
func Sum[T Number](s []T) T {
	var total T
	for _, v := range s {
		total += v
	}
	return total
}

// `cmp.Ordered` — все типы с операторами `<` и `>`.
func Max[T cmp.Ordered](a, b T) T {
	if a > b {
		return a
	}
	return b
}

type Celsius float64

// Ограничение может сочетать набор типов и методы:
// подходят только типы на основе `float64` с `String()`.
type Reading interface {
	~float64
	String() string
}

func (c Celsius) String() string { return fmt.Sprintf("%.1f°C", float64(c)) }

func Warmest[T Reading](rs []T) string {
	return slices.Max(rs).String()
}

type Employee struct {
	Name string
	Dept string
	Age  int
}

func main() {
	fmt.Println(Sum([]int{1, 2, 3}), Sum([]float64{0.5, 0.25}))

	// Благодаря `~` подходят и именованные типы.
	fmt.Println(Sum([]time.Duration{time.Second, 500 * time.Millisecond}))

	fmt.Println(Max(3, 7), Max("go", "c#"))
	fmt.Println(Warmest([]Celsius{21.5, 25, 19}))

	// `cmp.Compare` возвращает -1, 0 или +1. С ним удобно
	// сортировать структуры: `cmp.Or` берёт первый
	// ненулевой результат — сначала отдел, затем возраст.
	staff := []Employee{
		{"Bob", "ops", 41},
		{"Ann", "dev", 35},
		{"Eve", "dev", 29},
		{"Dan", "ops", 30},
	}
	slices.SortFunc(staff, func(a, b Employee) int {
		return cmp.Or(
			strings.Compare(a.Dept, b.Dept),
			cmp.Compare(a.Age, b.Age),
		)
	})
	for _, e := range staff {
		fmt.Println(e.Dept, e.Age, e.Name)
	}
}
