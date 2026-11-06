package task

// Uniq возвращает элементы s без повторов в порядке первого появления.
func Uniq[T comparable](s []T) []T {
	seen := make(map[T]struct{}, len(s))
	out := make([]T, 0, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}

// GroupBy группирует элементы s по ключу key.
func GroupBy[T any, K comparable](s []T, key func(T) K) map[K][]T {
	groups := make(map[K][]T)
	for _, v := range s {
		k := key(v)
		groups[k] = append(groups[k], v)
	}
	return groups
}

// Set — множество. Нулевое значение готово к работе.
type Set[T comparable] struct {
	m map[T]struct{}
}

// Add добавляет v в множество. map создаётся лениво,
// поэтому нулевое значение Set пригодно к работе.
func (s *Set[T]) Add(v T) {
	if s.m == nil {
		s.m = make(map[T]struct{})
	}
	s.m[v] = struct{}{}
}

// Has сообщает, есть ли v в множестве. Чтение из nil-map безопасно.
func (s *Set[T]) Has(v T) bool {
	_, ok := s.m[v]
	return ok
}

// Len возвращает количество элементов.
func (s *Set[T]) Len() int {
	return len(s.m)
}
