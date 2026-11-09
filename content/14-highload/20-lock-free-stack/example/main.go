// Стек Трайбера (Treiber, 1986) — самая простая lock-free
// структура: вершина — один `atomic.Pointer`, а `Push` и `Pop` —
// CAS-циклы. Горутины не ждут друг друга: если CAS не прошёл,
// значит, кто-то другой продвинулся, и надо перечитать вершину.

package main

import (
	"sync"
	"sync/atomic"
)

func main() {}

// Узел неизменяем после публикации: `next` записывается до CAS,
// пока узел видит только его создатель.
type node struct {
	v    int
	next *node
}

type Stack struct {
	head atomic.Pointer[node]
}

// Новый узел указывает на прочитанную вершину. CAS заменит
// вершину, только если она всё ещё `old`; иначе другой `Push`
// или `Pop` успел раньше — перечитываем и связываем заново.
func (s *Stack) Push(v int) {
	n := &node{v: v}
	for {
		old := s.head.Load()
		n.next = old
		if s.head.CompareAndSwap(old, n) {
			return
		}
	}
}

// `Pop` сдвигает вершину на `old.next`. Читать `old.next`
// безопасно: узел не меняется, а пока у нас есть указатель
// `old`, сборщик мусора его не освободит и память не будет
// переиспользована — так GC закрывает классическую проблему ABA.
func (s *Stack) Pop() (int, bool) {
	for {
		old := s.head.Load()
		if old == nil {
			return 0, false
		}
		if s.head.CompareAndSwap(old, old.next) {
			return old.v, true
		}
	}
}

// Для сравнения — стек на мьютексе и срезе. Он проще, не
// выделяет память на каждый `Push` и часто не медленнее.
type MutexStack struct {
	mu    sync.Mutex
	items []int
}

func (s *MutexStack) Push(v int) {
	s.mu.Lock()
	s.items = append(s.items, v)
	s.mu.Unlock()
}

func (s *MutexStack) Pop() (int, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.items) == 0 {
		return 0, false
	}
	v := s.items[len(s.items)-1]
	s.items = s.items[:len(s.items)-1]
	return v, true
}
