package task

// Uniq возвращает элементы s без повторов в порядке первого появления.
func Uniq[T comparable](s []T) []T {
	// TODO: реализуйте функцию
	return nil
}

// GroupBy группирует элементы s по ключу key.
func GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T {
	// TODO: реализуйте функцию
	return nil
}

// Set — множество. Нулевое значение готово к работе.
type Set[T comparable] struct {
	m map[T]struct{}
}

// Add добавляет v в множество.
func (s *Set[T]) Add(v T) {
	// TODO: реализуйте метод
}

// Has сообщает, есть ли v в множестве.
func (s *Set[T]) Has(v T) bool {
	// TODO: реализуйте метод
	return false
}

// Len возвращает количество элементов.
func (s *Set[T]) Len() int {
	// TODO: реализуйте метод
	return 0
}
