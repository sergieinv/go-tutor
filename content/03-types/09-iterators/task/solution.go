package task

import "iter"

// Filter возвращает только элементы seq, для которых keep вернул true.
func Filter[T any](seq iter.Seq[T], keep func(T) bool) iter.Seq[T] {
	return func(yield func(T) bool) {
		for v := range seq {
			// return из тела range по функции останавливает
			// и внутренний итератор seq.
			if keep(v) && !yield(v) {
				return
			}
		}
	}
}

// Take возвращает первые n элементов seq. При n <= 0 seq не вызывается.
func Take[T any](seq iter.Seq[T], n int) iter.Seq[T] {
	return func(yield func(T) bool) {
		if n <= 0 {
			return
		}
		// Счётчик заводится внутри: так итератор можно
		// обходить повторно.
		count := 0
		for v := range seq {
			if !yield(v) {
				return
			}
			count++
			if count == n {
				return // не просим у seq лишний элемент
			}
		}
	}
}

// Fib возвращает бесконечную последовательность Фибоначчи: 0, 1, 1, 2, 3, 5, …
func Fib() iter.Seq[int] {
	return func(yield func(int) bool) {
		a, b := 0, 1
		for {
			if !yield(a) {
				return
			}
			a, b = b, a+b
		}
	}
}
