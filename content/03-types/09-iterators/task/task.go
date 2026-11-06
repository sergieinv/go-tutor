package task

import "iter"

// Filter возвращает только элементы seq, для которых keep вернул true.
func Filter[T any](seq iter.Seq[T], keep func(T) bool) iter.Seq[T] {
	// TODO: реализуйте функцию
	return func(yield func(T) bool) {}
}

// Take возвращает первые n элементов seq. При n <= 0 seq не вызывается.
func Take[T any](seq iter.Seq[T], n int) iter.Seq[T] {
	// TODO: реализуйте функцию
	return func(yield func(T) bool) {}
}

// Fib возвращает бесконечную последовательность Фибоначчи: 0, 1, 1, 2, 3, 5, …
func Fib() iter.Seq[int] {
	// TODO: реализуйте функцию
	return func(yield func(int) bool) {}
}
