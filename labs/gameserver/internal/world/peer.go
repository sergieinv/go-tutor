package world

import (
	"sync"
	"sync/atomic"
)

// Buffer — пачка байт одного тика для одного клиента. Буферы берутся из
// пула и возвращаются горутиной записи после отправки: в установившемся
// режиме тик не аллоцирует память под исходящие данные.
type Buffer struct{ B []byte }

const maxPooledBuffer = 256 << 10 // большие буферы (пачки чанков) в пул не кладём

var bufferPool = sync.Pool{New: func() any { return &Buffer{B: make([]byte, 0, 2048)} }}

// GetBuffer берёт пустой буфер из пула.
func GetBuffer() *Buffer {
	b := bufferPool.Get().(*Buffer)
	b.B = b.B[:0]
	return b
}

// PutBuffer возвращает буфер в пул.
func PutBuffer(b *Buffer) {
	if b == nil || cap(b.B) > maxPooledBuffer {
		return
	}
	bufferPool.Put(b)
}

// Peer — сторона соединения, которую видит мир: протокол клиента и
// ограниченная очередь исходящих пачек.
//
// Мир кладёт в очередь не блокируясь (select с default): если клиент не
// успевает читать и очередь полна, мир отключает его, а не ждёт. Один
// медленный клиент не должен замедлять тик для всех.
type Peer struct {
	kind  Kind
	proto Protocol
	out   chan *Buffer

	closed    chan struct{}
	closeOnce sync.Once
	reason    atomic.Pointer[string]
}

// NewPeer создаёт сторону соединения с очередью на queueLen пачек
// (одна пачка — один тик, 64 пачки — около трёх секунд отставания).
func NewPeer(kind Kind, proto Protocol, queueLen int) *Peer {
	return &Peer{
		kind:   kind,
		proto:  proto,
		out:    make(chan *Buffer, queueLen),
		closed: make(chan struct{}),
	}
}

// Kind — тип клиента.
func (p *Peer) Kind() Kind { return p.kind }

// Out — очередь для горутины записи.
func (p *Peer) Out() <-chan *Buffer { return p.out }

// Closed закрывается, когда соединение нужно закрыть (мир отключил
// клиента или сервер останавливается).
func (p *Peer) Closed() <-chan struct{} { return p.closed }

// Close помечает соединение закрытым с причиной. Повторные вызовы ничего
// не делают: побеждает первая причина.
func (p *Peer) Close(reason string) {
	p.closeOnce.Do(func() {
		p.reason.Store(&reason)
		close(p.closed)
	})
}

// Reason — причина закрытия ("" — не закрыто).
func (p *Peer) Reason() string {
	if r := p.reason.Load(); r != nil {
		return *r
	}
	return ""
}

// trySend кладёт пачку в очередь без ожидания.
func (p *Peer) trySend(b *Buffer) bool {
	select {
	case p.out <- b:
		return true
	default:
		return false
	}
}

// QueueLen — сколько пачек ждут отправки (для тестов и отладки).
func (p *Peer) QueueLen() int { return len(p.out) }
