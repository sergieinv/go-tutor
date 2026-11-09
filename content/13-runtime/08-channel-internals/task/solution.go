package task

import "sync"

// waiter — аналог sudog: горутина, ждущая в Send или Recv.
type waiter[T any] struct {
	val  T          // что отправить или что получено
	ok   bool       // false — разбудил Close
	done bool       // операцию завершил партнёр или Close
	wake *sync.Cond // будит именно эту горутину
}

// Chan — канал с семантикой встроенного chan T на мьютексе.
type Chan[T any] struct {
	mu     sync.Mutex
	buf    []T // кольцевой буфер, len(buf) — ёмкость
	head   int // индекс самого старого элемента (recvx)
	n      int // элементов в буфере (qcount)
	recvq  []*waiter[T]
	sendq  []*waiter[T]
	closed bool
}

// New создаёт канал ёмкости capacity; 0 — небуферизованный.
func New[T any](capacity int) *Chan[T] {
	if capacity < 0 {
		panic("makechan: size out of range")
	}
	return &Chan[T]{buf: make([]T, capacity)}
}

func (c *Chan[T]) newWaiter(v T) *waiter[T] {
	return &waiter[T]{val: v, wake: sync.NewCond(&c.mu)}
}

// finish завершает операцию ждущей горутины и будит её.
func finish[T any](w *waiter[T], v T, ok bool) {
	w.val, w.ok, w.done = v, ok, true
	w.wake.Signal()
}

// Send отправляет v, блокируясь, пока значение не примут в буфер
// или не заберёт получатель. В закрытый канал — паника.
func (c *Chan[T]) Send(v T) {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		panic("send on closed channel")
	}
	// Ждущий получатель — значение уходит ему напрямую, мимо буфера.
	if len(c.recvq) > 0 {
		r := c.recvq[0]
		c.recvq = c.recvq[1:]
		finish(r, v, true)
		c.mu.Unlock()
		return
	}
	// Есть место в буфере.
	if c.n < len(c.buf) {
		c.buf[(c.head+c.n)%len(c.buf)] = v
		c.n++
		c.mu.Unlock()
		return
	}
	// Ждём в sendq: значение заберёт получатель или нас разбудит Close.
	w := c.newWaiter(v)
	c.sendq = append(c.sendq, w)
	for !w.done {
		w.wake.Wait()
	}
	ok := w.ok
	c.mu.Unlock()
	if !ok {
		panic("send on closed channel")
	}
}

// Recv получает значение. Из закрытого и пустого канала — нулевое значение и false.
func (c *Chan[T]) Recv() (T, bool) {
	var zero T
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.n > 0 {
		v := c.buf[c.head]
		c.buf[c.head] = zero // не держим ссылку для GC
		c.head = (c.head + 1) % len(c.buf)
		c.n--
		// Освободилось место: значение первого ждущего отправителя — в хвост.
		if len(c.sendq) > 0 {
			s := c.sendq[0]
			c.sendq = c.sendq[1:]
			c.buf[(c.head+c.n)%len(c.buf)] = s.val
			c.n++
			finish(s, zero, true)
		}
		return v, true
	}
	// Буфер пуст, но отправитель ждёт (небуферизованный канал) — забираем у него.
	if len(c.sendq) > 0 {
		s := c.sendq[0]
		c.sendq = c.sendq[1:]
		v := s.val
		finish(s, zero, true)
		return v, true
	}
	if c.closed {
		return zero, false
	}
	w := c.newWaiter(zero)
	c.recvq = append(c.recvq, w)
	for !w.done {
		w.wake.Wait()
	}
	return w.val, w.ok
}

// Close закрывает канал и будит всех ждущих: получатели получат
// нулевое значение и false, отправители запаникуют. Повторный Close — паника.
func (c *Chan[T]) Close() {
	var zero T
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		panic("close of closed channel")
	}
	c.closed = true
	for _, r := range c.recvq {
		finish(r, zero, false)
	}
	for _, s := range c.sendq {
		finish(s, zero, false)
	}
	c.recvq, c.sendq = nil, nil
}

// Len — число элементов в буфере.
func (c *Chan[T]) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.n
}

// Cap — ёмкость буфера.
func (c *Chan[T]) Cap() int {
	return len(c.buf)
}

// Waiting — сколько горутин сейчас ждут в Send и в Recv.
func (c *Chan[T]) Waiting() (senders, receivers int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.sendq), len(c.recvq)
}
