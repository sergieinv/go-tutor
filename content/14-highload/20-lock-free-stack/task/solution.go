package task

import "sync/atomic"

// node неизменяем после публикации: next записывается до CAS.
type node[T any] struct {
	v    T
	next *node[T]
}

// Stack — lock-free стек (Трайбера) на sync/atomic.
// Нулевое значение — пустой стек, готовый к работе.
type Stack[T any] struct {
	head atomic.Pointer[node[T]]
}

// Push кладёт v на вершину.
func (s *Stack[T]) Push(v T) {
	n := &node[T]{v: v}
	for {
		old := s.head.Load()
		n.next = old
		if s.head.CompareAndSwap(old, n) {
			return
		}
	}
}

// Pop снимает значение с вершины; для пустого стека — нулевое значение и false.
func (s *Stack[T]) Pop() (T, bool) {
	for {
		old := s.head.Load()
		if old == nil {
			var zero T
			return zero, false
		}
		if s.head.CompareAndSwap(old, old.next) {
			return old.v, true
		}
	}
}

// PushMany кладёт все значения одной атомарной операцией: последнее
// оказывается на вершине. Цепочка собирается заранее, пока её никто
// не видит; при повторе CAS меняется только next нижнего узла.
func (s *Stack[T]) PushMany(vs ...T) {
	if len(vs) == 0 {
		return
	}
	bottom := &node[T]{v: vs[0]}
	top := bottom
	for _, v := range vs[1:] {
		top = &node[T]{v: v, next: top}
	}
	for {
		old := s.head.Load()
		bottom.next = old
		if s.head.CompareAndSwap(old, top) {
			return
		}
	}
}

// Drain атомарно забирает всё содержимое стека и возвращает его от
// вершины ко дну; для пустого стека — nil. Swap отрывает всю цепочку
// разом: узлы больше никто не изменит, обходить их можно без атомиков.
func (s *Stack[T]) Drain() []T {
	var out []T
	for n := s.head.Swap(nil); n != nil; n = n.next {
		out = append(out, n.v)
	}
	return out
}
