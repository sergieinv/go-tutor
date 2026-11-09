package task

import (
	"bytes"
	"sync"
	"sync/atomic"
)

// BufferPool — пул буферов поверх sync.Pool, который не держит
// слишком большие буферы и всегда выдаёт пустой буфер.
type BufferPool struct {
	pool    sync.Pool
	maxCap  int
	dropped atomic.Int64
}

// NewBufferPool создаёт пул. Новые буферы получают ёмкость не меньше
// initCap; буферы с ёмкостью больше maxCap в пул не возвращаются.
func NewBufferPool(initCap, maxCap int) *BufferPool {
	initCap = max(initCap, 0)
	maxCap = max(maxCap, initCap)
	p := &BufferPool{maxCap: maxCap}
	p.pool.New = func() any {
		return bytes.NewBuffer(make([]byte, 0, initCap))
	}
	return p
}

// Get возвращает пустой буфер: из пула или новый.
func (p *BufferPool) Get() *bytes.Buffer {
	return p.pool.Get().(*bytes.Buffer)
}

// Put возвращает буфер в пул (или выбрасывает слишком большой).
func (p *BufferPool) Put(b *bytes.Buffer) {
	if b == nil {
		return
	}
	if b.Cap() > p.maxCap {
		// Огромный буфер в пуле — это память, которую держат
		// ради редкого случая. Пусть его заберёт GC.
		p.dropped.Add(1)
		return
	}
	b.Reset()
	p.pool.Put(b)
}

// Dropped возвращает, сколько буферов Put выбросил из-за размера.
func (p *BufferPool) Dropped() int64 {
	return p.dropped.Load()
}
