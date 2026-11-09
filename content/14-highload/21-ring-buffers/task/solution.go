package task

import (
	"math/bits"
	"sync/atomic"
)

// CacheLine — минимальное расстояние в байтах между head и tail.
const CacheLine = 128

// Ring — кольцевой буфер для одного производителя и одного потребителя.
// Индексы растут без ограничения, позиция в buf — индекс по маске.
// Каждый индекс вместе с полями своей стороны — в своей кеш-линии.
type Ring[T any] struct {
	head       atomic.Uint64 // сколько элементов прочитано; пишет только потребитель
	cachedTail uint64        // последний tail, который видел потребитель
	_          [CacheLine - 16]byte
	tail       atomic.Uint64 // сколько элементов записано; пишет только производитель
	cachedHead uint64        // последний head, который видел производитель
	_          [CacheLine - 16]byte
	buf        []T
	mask       uint64
}

// New создаёт кольцо ёмкостью capacity, округлённой вверх до степени
// двойки; capacity < 1 считается равной 1.
func New[T any](capacity int) *Ring[T] {
	size := 1
	if capacity > 1 {
		size = 1 << bits.Len(uint(capacity-1))
	}
	return &Ring[T]{buf: make([]T, size), mask: uint64(size - 1)}
}

// Cap возвращает ёмкость кольца.
func (r *Ring[T]) Cap() int { return len(r.buf) }

// Len возвращает число элементов. head читается раньше tail: tail
// к этому моменту не меньше head. Пока другая сторона работает,
// разность может обогнать ёмкость — ограничиваем её.
func (r *Ring[T]) Len() int {
	h := r.head.Load()
	t := r.tail.Load()
	return int(min(t-h, uint64(len(r.buf))))
}

// TryPush кладёт v в конец; false — если кольцо полно. Только для производителя.
// Чужой head перечитывается, только когда по закешированному значению
// места нет: так линия потребителя трогается редко.
func (r *Ring[T]) TryPush(v T) bool {
	t := r.tail.Load()
	if t-r.cachedHead == uint64(len(r.buf)) {
		r.cachedHead = r.head.Load()
		if t-r.cachedHead == uint64(len(r.buf)) {
			return false
		}
	}
	r.buf[t&r.mask] = v
	r.tail.Store(t + 1) // публикует значение в ячейке
	return true
}

// TryPop забирает элемент из начала; нулевое значение и false — если
// кольцо пусто. Только для потребителя. Ячейка обнуляется до сдвига
// head: кольцо не держит выданный элемент живым для GC.
func (r *Ring[T]) TryPop() (T, bool) {
	var zero T
	h := r.head.Load()
	if h == r.cachedTail {
		r.cachedTail = r.tail.Load()
		if h == r.cachedTail {
			return zero, false
		}
	}
	i := h & r.mask
	v := r.buf[i]
	r.buf[i] = zero
	r.head.Store(h + 1) // с этого момента ячейку может занять производитель
	return v, true
}
