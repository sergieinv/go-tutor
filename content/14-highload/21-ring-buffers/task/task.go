package task

import "sync/atomic"

// CacheLine — минимальное расстояние в байтах между head и tail.
const CacheLine = 128

// Ring — кольцевой буфер для одного производителя и одного потребителя.
type Ring[T any] struct {
	head atomic.Uint64 // сколько элементов прочитано; пишет только потребитель
	tail atomic.Uint64 // сколько элементов записано; пишет только производитель
	// TODO: заполнители и остальные поля
}

// New создаёт кольцо ёмкостью capacity, округлённой вверх до степени
// двойки; capacity < 1 считается равной 1.
func New[T any](capacity int) *Ring[T] {
	// TODO
	return &Ring[T]{}
}

// Cap возвращает ёмкость кольца.
func (r *Ring[T]) Cap() int {
	// TODO
	return 0
}

// Len возвращает число элементов в кольце.
func (r *Ring[T]) Len() int {
	// TODO
	return 0
}

// TryPush кладёт v в конец; false — если кольцо полно. Только для производителя.
func (r *Ring[T]) TryPush(v T) bool {
	// TODO
	return false
}

// TryPop забирает элемент из начала; нулевое значение и false — если
// кольцо пусто. Только для потребителя.
func (r *Ring[T]) TryPop() (T, bool) {
	// TODO
	var zero T
	return zero, false
}
