package task

import "sync"

// Chan — канал с семантикой встроенного chan T на мьютексе.
// Встроенные каналы (chan, <-, select) здесь использовать нельзя.
type Chan[T any] struct {
	mu sync.Mutex
	// TODO: буфер, очереди ждущих, флаг закрытия
}

// New создаёт канал ёмкости capacity; 0 — небуферизованный.
func New[T any](capacity int) *Chan[T] {
	// TODO: реализуйте
	return &Chan[T]{}
}

// Send отправляет v, блокируясь, пока значение не примут в буфер
// или не заберёт получатель. В закрытый канал — паника.
func (c *Chan[T]) Send(v T) {
	// TODO: реализуйте
}

// Recv получает значение. Из закрытого и пустого канала — нулевое значение и false.
func (c *Chan[T]) Recv() (T, bool) {
	// TODO: реализуйте
	var zero T
	return zero, false
}

// Close закрывает канал и будит всех ждущих: получатели получат
// нулевое значение и false, отправители запаникуют. Повторный Close — паника.
func (c *Chan[T]) Close() {
	// TODO: реализуйте
}

// Len — число элементов в буфере.
func (c *Chan[T]) Len() int {
	// TODO: реализуйте
	return 0
}

// Cap — ёмкость буфера.
func (c *Chan[T]) Cap() int {
	// TODO: реализуйте
	return 0
}

// Waiting — сколько горутин сейчас ждут в Send и в Recv.
func (c *Chan[T]) Waiting() (senders, receivers int) {
	// TODO: реализуйте
	return 0, 0
}
