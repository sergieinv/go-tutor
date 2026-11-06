package task

import "slices"

// Stack — стек целых чисел. Нулевое значение — пустой стек.
type Stack struct {
	items []int
}

// NewStack создаёт стек из items; последний элемент — вершина.
func NewStack(items ...int) *Stack {
	// Срез variadic-параметра может быть срезом вызывающего
	// (NewStack(s...)), поэтому копируем.
	return &Stack{items: slices.Clone(items)}
}

// Push кладёт v на вершину. Pointer receiver: метод меняет стек.
func (s *Stack) Push(v int) {
	s.items = append(s.items, v)
}

// Pop снимает вершину. На пустом стеке — 0, false.
func (s *Stack) Pop() (int, bool) {
	if len(s.items) == 0 {
		return 0, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}

// Peek возвращает вершину без снятия. На пустом стеке — 0, false.
func (s *Stack) Peek() (int, bool) {
	if len(s.items) == 0 {
		return 0, false
	}
	return s.items[len(s.items)-1], true
}

// Len возвращает количество элементов.
func (s *Stack) Len() int {
	return len(s.items)
}
