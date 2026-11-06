// Рекурсия в Go — как везде: функция вызывает себя
// и обязательно имеет базовый случай. Стек горутины
// растёт динамически, поэтому глубокая рекурсия не
// упирается в фиксированный 1 МБ, как поток в .NET.

package main

import (
	"fmt"
	"strings"
)

// Классический пример: факториал.
func fact(n int) int {
	if n <= 1 {
		return 1
	}
	return n * fact(n-1)
}

// Дерево из указателей: `nil` — пустое поддерево
// и естественный базовый случай рекурсии.
type tree struct {
	val         string
	left, right *tree
}

// Обход в глубину: печатаем узел с отступом
// по уровню, затем поддеревья.
func printTree(t *tree, level int) {
	if t == nil {
		return
	}
	fmt.Println(strings.Repeat("  ", level) + t.val)
	printTree(t.left, level+1)
	printTree(t.right, level+1)
}

// Сто тысяч вложенных вызовов — для Go не проблема:
// когда стек кончается, рантайм выделяет вдвое больший
// и копирует туда кадры.
func depth(n int) int {
	if n == 0 {
		return 0
	}
	return depth(n-1) + 1
}

func main() {
	fmt.Println("5! =", fact(5))

	root := &tree{"a",
		&tree{"b", &tree{"d", nil, nil}, nil},
		&tree{"c", nil, &tree{"e", nil, nil}},
	}
	printTree(root, 0)

	fmt.Println("depth:", depth(100_000))

	// Литерал функции не видит сам себя по имени:
	// сначала объявляем переменную, потом присваиваем
	// замыкание, которое её вызывает.
	var count func(t *tree) int
	count = func(t *tree) int {
		if t == nil {
			return 0
		}
		return 1 + count(t.left) + count(t.right)
	}
	fmt.Println("nodes:", count(root))

	// Взаимная рекурсия между функциями пакета работает
	// без объявлений заранее — порядок в файле не важен.
	fmt.Println(isEven(10), isOdd(7))
}

func isEven(n int) bool {
	if n == 0 {
		return true
	}
	return isOdd(n - 1)
}

func isOdd(n int) bool {
	if n == 0 {
		return false
	}
	return isEven(n - 1)
}
