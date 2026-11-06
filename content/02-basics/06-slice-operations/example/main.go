// У среза нет методов: копирование, поиск, сортировку
// и вставку делают встроенная `copy` и обобщённые
// функции пакета `slices`.

package main

import (
	"fmt"
	"slices"
)

func main() {

	// `copy(dst, src)` копирует `min(len(dst), len(src))`
	// элементов и возвращает их число. Память не
	// выделяет — приёмник готовят заранее.
	src := []int{1, 2, 3, 4, 5}
	dst := make([]int, 3)
	n := copy(dst, src)
	fmt.Println("copy:", n, dst)

	// Поиск и сравнение. `Equal` считает `nil`-срез
	// и пустой срез равными.
	langs := []string{"go", "rust", "c#"}
	fmt.Println("contains:", slices.Contains(langs, "rust"))
	fmt.Println("index:", slices.Index(langs, "c#"), slices.Index(langs, "java"))
	fmt.Println("equal:", slices.Equal([]int(nil), []int{}))

	// `Sort` сортирует на месте. Если исходный порядок
	// нужен, сначала `Clone` — независимая копия.
	sorted := slices.Clone(langs)
	slices.Sort(sorted)
	fmt.Println("sort:", sorted, "orig:", langs)

	// `Insert` и `Delete` возвращают новый заголовок —
	// результат присваиваем. `Delete(s, i, j)` удаляет
	// элементы с индексами от i до j-1.
	nums := []int{10, 20, 30, 40, 50}
	nums = slices.Insert(nums, 1, 15)
	fmt.Println("insert:", nums)
	nums = slices.Delete(nums, 2, 4)
	fmt.Println("delete:", nums)

	// Удаление без сохранения порядка — за O(1):
	// последний элемент переносим на место удаляемого.
	ids := []int{7, 8, 9, 10}
	i := 1
	ids[i] = ids[len(ids)-1]
	ids = ids[:len(ids)-1]
	fmt.Println("swap-delete:", ids)

	// Полное выражение `s[low:high:max]` задаёт ещё
	// и ёмкость: `max - low`. Без запаса ёмкости
	// `append` не тронет исходный массив.
	base := []int{1, 2, 3, 4, 5}
	head := base[0:2:2]
	fmt.Println("full slice:", len(head), cap(head))
	head = append(head, 99)
	fmt.Println("after append:", head, base)

	// Двумерный срез — срез срезов. Чтобы строки лежали
	// в памяти подряд, их нарезают из одного массива.
	rows, cols := 2, 3
	cells := make([]int, rows*cols)
	grid := make([][]int, rows)
	for r := range grid {
		grid[r] = cells[r*cols : (r+1)*cols : (r+1)*cols]
	}
	grid[1][2] = 7
	fmt.Println("grid:", grid, "cells:", cells)
}
