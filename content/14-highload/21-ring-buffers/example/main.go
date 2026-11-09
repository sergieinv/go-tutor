// Кольцевой буфер — массив фиксированной длины и два
// растущих индекса: `head` (откуда читать) и `tail` (куда
// писать). Позиция в массиве — индекс по маске, поэтому длина —
// степень двойки. Когда у каждого индекса один писатель, кольцо
// обходится без блокировок и даже без CAS.

package main

import "sync/atomic"

func main() {}

const CacheLine = 128

// SPSC — один производитель, один потребитель. `tail` пишет
// только производитель, `head` — только потребитель; каждый
// индекс в своей кеш-линии, чтобы они не мешали друг другу.
type SPSC struct {
	head atomic.Uint64
	_    [CacheLine - 8]byte
	tail atomic.Uint64
	_    [CacheLine - 8]byte
	buf  []int
	mask uint64
}

func NewSPSC(size int) *SPSC { // size — степень двойки
	return &SPSC{buf: make([]int, size), mask: uint64(size - 1)}
}

// Сначала пишем значение в ячейку, потом публикуем `tail`.
// Атомарная запись `tail` «synchronizes before» чтение, которое
// её увидит, — потребитель гарантированно прочитает значение.
func (q *SPSC) TryPush(v int) bool {
	t := q.tail.Load()
	if t-q.head.Load() == uint64(len(q.buf)) {
		return false // полно
	}
	q.buf[t&q.mask] = v
	q.tail.Store(t + 1)
	return true
}

// Зеркально: прочитать ячейку, потом сдвинуть `head` — с этого
// момента производитель может её перезаписать.
func (q *SPSC) TryPop() (int, bool) {
	h := q.head.Load()
	if h == q.tail.Load() {
		return 0, false // пусто
	}
	v := q.buf[h&q.mask]
	q.head.Store(h + 1)
	return v, true
}

// MPSC — много производителей. Теперь `tail` пишут все, и
// ячейку занимают CAS. Но после CAS значение ещё не записано —
// поэтому у каждой ячейки свой номер `seq` (схема Дмитрия
// Вьюкова): `seq == i` — ячейка свободна для записи номер i,
// `seq == i+1` — значение i готово к чтению.
type slot struct {
	seq atomic.Uint64
	v   int
}

type MPSC struct {
	tail atomic.Uint64
	_    [CacheLine - 8]byte
	head uint64 // только потребитель
	buf  []slot
	mask uint64
}

func NewMPSC(size int) *MPSC {
	q := &MPSC{buf: make([]slot, size), mask: uint64(size - 1)}
	for i := range q.buf {
		q.buf[i].seq.Store(uint64(i))
	}
	return q
}

func (q *MPSC) TryPush(v int) bool {
	for {
		t := q.tail.Load()
		s := &q.buf[t&q.mask]
		switch seq := s.seq.Load(); {
		case seq == t && q.tail.CompareAndSwap(t, t+1):
			s.v = v
			s.seq.Store(t + 1) // опубликовать значение
			return true
		case seq < t:
			return false // ячейку ещё не прочитали с прошлого круга: полно
		}
		// другой производитель успел раньше — перечитываем tail
	}
}

func (q *MPSC) TryPop() (int, bool) {
	s := &q.buf[q.head&q.mask]
	if s.seq.Load() != q.head+1 {
		return 0, false // пусто или производитель ещё пишет
	}
	v := s.v
	s.seq.Store(q.head + uint64(len(q.buf))) // свободна на следующем круге
	q.head++
	return v, true
}
