// Срез — это «окно» в массив: указатель на первый
// элемент, длина и ёмкость. В отличие от `List<T>`,
// срез — значение из трёх полей, которое копируется
// при присваивании, а массив под ним остаётся общим.

package main

import (
	"fmt"
	"slices"
)

func main() {

	// Неинициализированный срез равен `nil`, его длина 0.
	// `nil`-срез можно обходить в цикле и передавать в `append`.
	var s []string
	fmt.Println("nil:", s == nil, len(s))

	// `make` создаёт срез заданной длины. Элементы
	// получают нулевые значения — здесь пустые строки.
	s = make([]string, 3)
	fmt.Println("make:", s, "len:", len(s), "cap:", cap(s))

	s[0] = "a"
	s[1] = "b"
	s[2] = "c"
	fmt.Println("set:", s)

	// `append` возвращает новый заголовок среза — результат
	// обязательно присваивать. Если ёмкости не хватает,
	// рантайм выделяет массив побольше и копирует элементы.
	s = append(s, "d")
	s = append(s, "e", "f")
	fmt.Println("append:", s, "len:", len(s), "cap:", cap(s))

	// `s[2:5]` — элементы с индекса 2 по 4. Данные не
	// копируются: новый срез смотрит в тот же массив.
	l := s[2:5]
	fmt.Println("slice:", l)
	l[0] = "C"
	fmt.Println("shared:", s)

	// Третий индекс ограничивает ёмкость: у `p` нет места
	// для роста, поэтому `append` выделит новый массив
	// и не затронет `s`.
	p := s[1:3:3]
	p = append(p, "X")
	fmt.Println("full slice:", p, "s:", s)

	// `copy` копирует элементы в независимый срез.
	c := make([]string, len(s))
	copy(c, s)
	c[0] = "A"
	fmt.Println("copy:", c[0], s[0])

	// Пакет `slices` закрывает то, что в C# делают
	// методы `List<T>` и LINQ: сравнение, поиск, копия.
	t := []string{"g", "h", "i"}
	fmt.Println("equal:", slices.Equal(t, []string{"g", "h", "i"}))
	fmt.Println("index:", slices.Index(t, "h"))
	fmt.Println("clone:", slices.Clone(t))

	// Внутренние срезы двумерного среза могут быть
	// разной длины — это не прямоугольная матрица.
	twoD := make([][]int, 3)
	for i := range 3 {
		twoD[i] = make([]int, i+1)
		for j := range i + 1 {
			twoD[i][j] = i + j
		}
	}
	fmt.Println("2d:", twoD)
}
