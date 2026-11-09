package task

import (
	"container/list"
	"iter"
)

// Queue — FIFO-очередь идентификаторов задач. Сейчас она на
// container/list: каждый Push выделяет узел в куче, а обход прыгает
// по указателям. Перепишите её на срезе. Нулевое значение Queue —
// пустая очередь, готовая к работе.
type Queue struct {
	l list.List
}

// Push добавляет v в конец очереди.
func (q *Queue) Push(v int64) { q.l.PushBack(v) }

// Pop убирает и возвращает первый элемент; если очередь пуста — 0, false.
func (q *Queue) Pop() (int64, bool) {
	e := q.l.Front()
	if e == nil {
		return 0, false
	}
	q.l.Remove(e)
	return e.Value.(int64), true
}

// Peek возвращает первый элемент, не убирая его; если пусто — 0, false.
func (q *Queue) Peek() (int64, bool) {
	e := q.l.Front()
	if e == nil {
		return 0, false
	}
	return e.Value.(int64), true
}

// Len — число элементов в очереди.
func (q *Queue) Len() int { return q.l.Len() }

// All обходит элементы от первого к последнему, не убирая их.
func (q *Queue) All() iter.Seq[int64] {
	return func(yield func(int64) bool) {
		for e := q.l.Front(); e != nil; e = e.Next() {
			if !yield(e.Value.(int64)) {
				return
			}
		}
	}
}
