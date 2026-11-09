package task

import "bytes"

// BufferPool — пул буферов поверх sync.Pool, который не держит
// слишком большие буферы и всегда выдаёт пустой буфер.
type BufferPool struct {
	// TODO: поля
}

// NewBufferPool создаёт пул. Новые буферы получают ёмкость не меньше
// initCap; буферы с ёмкостью больше maxCap в пул не возвращаются.
func NewBufferPool(initCap, maxCap int) *BufferPool {
	// TODO: реализуйте конструктор
	return &BufferPool{}
}

// Get возвращает пустой буфер: из пула или новый.
func (p *BufferPool) Get() *bytes.Buffer {
	// TODO: реализуйте метод
	return new(bytes.Buffer)
}

// Put возвращает буфер в пул (или выбрасывает слишком большой).
func (p *BufferPool) Put(b *bytes.Buffer) {
	// TODO: реализуйте метод
}

// Dropped возвращает, сколько буферов Put выбросил из-за размера.
func (p *BufferPool) Dropped() int64 {
	// TODO: реализуйте метод
	return 0
}
