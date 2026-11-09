package task

import (
	"math/bits"
	"math/rand/v2"
	"runtime"
	"sync/atomic"
	"unsafe"
)

// CacheLine — размер, до которого выравнивается каждый шард.
const CacheLine = 128

// shard — одна часть счётчика. Поле n должно остаться первым.
// Заполнитель добивает шард до CacheLine байт: счётчики соседних
// шардов отстоят друг от друга на целую линию и не делят её.
type shard struct {
	n atomic.Int64
	_ [CacheLine - unsafe.Sizeof(atomic.Int64{})]byte
}

// Counter — счётчик, разделённый на шарды.
type Counter struct {
	shards []shard
}

// New создаёт счётчик. Число шардов — n, округлённое вверх до
// степени двойки; при n <= 0 вместо n берётся runtime.GOMAXPROCS(0).
func New(n int) *Counter {
	if n <= 0 {
		n = runtime.GOMAXPROCS(0)
	}
	size := 1
	if n > 1 {
		size = 1 << bits.Len(uint(n-1))
	}
	return &Counter{shards: make([]shard, size)}
}

// Shards возвращает число шардов.
func (c *Counter) Shards() int { return len(c.shards) }

// AddAt прибавляет delta к шарду с индексом hint & (Shards()-1).
// Число шардов — степень двойки, поэтому маска работает как
// остаток от деления и для отрицательных hint.
func (c *Counter) AddAt(hint int, delta int64) {
	c.shards[hint&(len(c.shards)-1)].n.Add(delta)
}

// Add прибавляет delta к случайному шарду. Номера горутины в Go
// нет; генератор math/rand/v2 без блокировок — у каждого
// потока ОС своё состояние.
func (c *Counter) Add(delta int64) {
	c.AddAt(int(rand.Uint32()), delta)
}

// Load возвращает сумму всех шардов. Это не снимок: шарды
// читаются по очереди, пока другие горутины продолжают писать.
func (c *Counter) Load() int64 {
	var sum int64
	for i := range c.shards {
		sum += c.shards[i].n.Load()
	}
	return sum
}
