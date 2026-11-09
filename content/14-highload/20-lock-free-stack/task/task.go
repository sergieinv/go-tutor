package task

// Stack — lock-free стек (Трайбера) на sync/atomic.
// Нулевое значение — пустой стек, готовый к работе.
type Stack[T any] struct {
	// TODO: вершина стека
}

// Push кладёт v на вершину.
func (s *Stack[T]) Push(v T) {
	// TODO
}

// Pop снимает значение с вершины; для пустого стека — нулевое значение и false.
func (s *Stack[T]) Pop() (T, bool) {
	// TODO
	var zero T
	return zero, false
}

// PushMany кладёт все значения одной атомарной операцией: последнее
// оказывается на вершине.
func (s *Stack[T]) PushMany(vs ...T) {
	// TODO
}

// Drain атомарно забирает всё содержимое стека и возвращает его от
// вершины ко дну; для пустого стека — nil.
func (s *Stack[T]) Drain() []T {
	// TODO
	return nil
}
