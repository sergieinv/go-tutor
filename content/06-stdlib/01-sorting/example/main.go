// Сортировка в Go — функции пакета `slices` (Go 1.21+).
// Они сортируют срез на месте и ничего не возвращают:
// никакого нового «отсортированного представления».

package main

import (
	"cmp"
	"fmt"
	"slices"
	"sort"
	"strings"
)

type user struct {
	name string
	dept string
	age  int
}

func main() {
	// `slices.Sort` работает для любых упорядоченных типов
	// (`cmp.Ordered`): чисел и строк. Строки сравниваются
	// побайтно — заглавные латинские буквы идут раньше строчных.
	nums := []int{5, 2, 8, 1}
	slices.Sort(nums)
	words := []string{"banana", "Cherry", "apple"}
	slices.Sort(words)
	fmt.Println(nums, words)

	// Для своих правил — `slices.SortFunc` с функцией
	// сравнения: отрицательное число — `a` раньше `b`, ноль —
	// равны, положительное — `a` позже. `cmp.Compare` возвращает
	// ровно это и не переполняется, в отличие от `a - b`.
	users := []user{
		{"Олег", "ops", 41}, {"Анна", "dev", 29},
		{"Иван", "dev", 25}, {"Вера", "ops", 29},
	}
	slices.SortFunc(users, func(a, b user) int {
		return cmp.Compare(b.age, a.age) // b, a — по убыванию
	})
	fmt.Println(users)

	// Несколько ключей — `cmp.Or`: он возвращает первый
	// ненулевой аргумент. Это аналог `OrderBy(...).ThenBy(...)`.
	slices.SortFunc(users, func(a, b user) int {
		return cmp.Or(
			cmp.Compare(a.dept, b.dept),
			cmp.Compare(a.age, b.age),
			strings.Compare(a.name, b.name),
		)
	})
	fmt.Println(users)

	// `SortFunc` неустойчив: порядок равных элементов
	// не гарантирован. `SortStableFunc` его сохраняет: сначала
	// сортируем по имени, затем устойчиво по отделу — внутри
	// отдела имена остаются по алфавиту.
	slices.SortFunc(users, func(a, b user) int {
		return strings.Compare(a.name, b.name)
	})
	slices.SortStableFunc(users, func(a, b user) int {
		return cmp.Compare(a.dept, b.dept)
	})
	fmt.Println(users)

	// Двоичный поиск — только по срезу, отсортированному
	// в том же порядке. Возвращает позицию и флаг «нашёл»;
	// если не нашёл — позицию, куда элемент надо вставить.
	i, found := slices.BinarySearch(nums, 5)
	j, found2 := slices.BinarySearch(nums, 6)
	fmt.Println(i, found, j, found2)
	fmt.Println(slices.IsSorted(nums), slices.Max(nums))

	// Пакет `sort` — наследие до generics: `sort.Ints`,
	// `sort.Slice` с функцией «меньше» по индексам. В новом
	// коде используйте `slices`: он типобезопасен и быстрее.
	legacy := []int{3, 1, 2}
	sort.Slice(legacy, func(i, j int) bool { return legacy[i] < legacy[j] })
	fmt.Println(legacy)
}
