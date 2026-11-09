package task

import "sync/atomic"

// CacheLine — размер, до которого выравнивается каждый шард.
const CacheLine = 128

// shard — одна часть счётчика. Поле n должно остаться первым.
type shard struct {
	n atomic.Int64
	// TODO: добейте шард до CacheLine байт.
}

// Counter — счётчик, разделённый на шарды.
type Counter struct {
	shards []shard
}

// New создаёт счётчик. Число шардов — n, округлённое вверх до
// степени двойки; при n <= 0 вместо n берётся runtime.GOMAXPROCS(0).
func New(n int) *Counter {
	// TODO
	return &Counter{}
}

// Shards возвращает число шардов.
func (c *Counter) Shards() int {
	// TODO
	return 0
}

// AddAt прибавляет delta к шарду с индексом hint & (Shards()-1).
func (c *Counter) AddAt(hint int, delta int64) {
	// TODO
}

// Add прибавляет delta к какому-нибудь шарду.
func (c *Counter) Add(delta int64) {
	// TODO
}

// Load возвращает сумму всех шардов.
func (c *Counter) Load() int64 {
	// TODO
	return 0
}
