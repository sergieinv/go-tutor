package main

import "math/rand/v2"

// Один и тот же обход данных в разной раскладке. Результат всегда
// одинаковый — меняется только то, сколько раз процессор ждёт память.
func main() {}

// Срез — значения подряд: каждая кеш-линия (64 байта на x86-64,
// 128 на Apple M) приносит 8 или 16 соседних `int64`, а аппаратная
// предвыборка (prefetch) замечает последовательный обход и подтягивает
// следующие линии заранее.
func sumSlice(xs []int64) int64 {
	var s int64
	for _, x := range xs {
		s += x
	}
	return s
}

// Связный список: адрес следующего узла известен, только когда
// прочитан текущий. Предвыборке не за что зацепиться, и если узлы
// разбросаны по памяти, каждый шаг — промах кеша.
type node struct {
	val  int64
	next *node
}

func sumList(head *node) int64 {
	var s int64
	for n := head; n != nil; n = n.next {
		s += n.val
	}
	return s
}

// buildList связывает узлы в порядке order. Узлы лежат в одном
// массиве, так что разница только в порядке обхода: подряд или
// вразнобой — как у списка, который долго жил, рос и перестраивался.
func buildList(vals []int64, order []int) *node {
	nodes := make([]node, len(vals))
	var head *node
	for i := len(order) - 1; i >= 0; i-- {
		n := &nodes[order[i]]
		n.val = vals[order[i]]
		n.next = head
		head = n
	}
	return head
}

// Порядок «подряд» и детерминированная перестановка: фиксированный
// сид, чтобы тесты и бенчмарки были воспроизводимы.
func sequential(n int) []int {
	order := make([]int, n)
	for i := range order {
		order[i] = i
	}
	return order
}

func shuffled(n int) []int {
	order := sequential(n)
	r := rand.New(rand.NewPCG(1, 2))
	r.Shuffle(n, func(i, j int) { order[i], order[j] = order[j], order[i] })
	return order
}

// Матрица n×n в одном срезе по строкам (row-major): элемент (i, j)
// лежит по индексу i*n+j. Обход по строкам идёт по памяти подряд.
func sumRows(m []int64, n int) int64 {
	var s int64
	for i := range n {
		for _, x := range m[i*n : (i+1)*n] {
			s += x
		}
	}
	return s
}

// Обход по столбцам: соседние шаги отстоят на n*8 байт, каждая
// загруженная линия даёт один нужный элемент, остальные вытесняются
// раньше, чем до них дойдёт очередь.
func sumCols(m []int64, n int) int64 {
	var s int64
	for j := range n {
		for i := range n {
			s += m[i*n+j]
		}
	}
	return s
}
