package task

// Stack — стек целых чисел. Нулевое значение — пустой стек.
type Stack struct {
	items []int
}

// NewStack создаёт стек из items; последний элемент — вершина.
func NewStack(items ...int) *Stack {
	// TODO: реализуйте функцию
	return &Stack{}
}

// Push кладёт v на вершину.
func (s *Stack) Push(v int) {
	// TODO: реализуйте метод
}

// Pop снимает вершину. На пустом стеке — 0, false.
func (s *Stack) Pop() (int, bool) {
	// TODO: реализуйте метод
	return 0, false
}

// Peek возвращает вершину без снятия. На пустом стеке — 0, false.
func (s *Stack) Peek() (int, bool) {
	// TODO: реализуйте метод
	return 0, false
}

// Len возвращает количество элементов.
func (s *Stack) Len() int {
	// TODO: реализуйте метод
	return 0
}
