package task

import (
	"bytes"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

var errConnClosed = errors.New("fake: use of closed connection")

// fakeConn — соединение под управлением теста. Каждый Write
// записывается и сигналит в entered; если block не nil, Write ждёт,
// пока тест закроет block (или, при closeUnblocks, пока не вызовут Close).
type fakeConn struct {
	mu            sync.Mutex
	writes        [][]byte
	closes        int
	entered       chan struct{}
	block         chan struct{}
	closed        chan struct{}
	closeUnblocks bool
	err           error
}

func newFakeConn(block, closeUnblocks bool) *fakeConn {
	f := &fakeConn{entered: make(chan struct{}, 10000), closed: make(chan struct{}), closeUnblocks: closeUnblocks}
	if block {
		f.block = make(chan struct{})
	}
	return f
}

func (f *fakeConn) Write(p []byte) (int, error) {
	f.mu.Lock()
	f.writes = append(f.writes, bytes.Clone(p))
	err := f.err
	f.mu.Unlock()
	f.entered <- struct{}{}
	if f.block != nil {
		var closed chan struct{}
		if f.closeUnblocks {
			closed = f.closed
		}
		select {
		case <-f.block:
		case <-closed:
			return 0, errConnClosed
		}
	}
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

func (f *fakeConn) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closes++
	if f.closes == 1 {
		close(f.closed)
	}
	return nil
}

func (f *fakeConn) stats() (writes [][]byte, closes int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]byte(nil), f.writes...), f.closes
}

// waitFor ждёт канал не дольше 2 секунд.
func waitFor(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	if ch == nil {
		t.Fatalf("не дождались: %s — канал nil", what)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatalf("не дождались: %s", what)
	}
}

// reasons — onClose, который запоминает все вызовы.
type reasons struct {
	mu   sync.Mutex
	list []error
}

func (r *reasons) record(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.list = append(r.list, err)
}

func (r *reasons) get() []error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]error(nil), r.list...)
}

func TestDeliversInOrder(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	c := NewClient(server, 4, nil)
	msgs := []string{"chunk 0 0", "move alice", "chat hi"}
	for _, m := range msgs {
		if !c.Send([]byte(m)) {
			t.Fatalf("Send(%q) = false", m)
		}
	}
	for _, m := range msgs {
		buf := make([]byte, len(m))
		client.SetReadDeadline(time.Now().Add(2 * time.Second))
		if _, err := io.ReadFull(client, buf); err != nil || string(buf) != m {
			t.Fatalf("прочитано %q, %v; want %q", buf, err, m)
		}
	}
	if c.Err() != nil {
		t.Fatalf("Err() = %v до закрытия, want nil", c.Err())
	}
	c.Close(nil)
	waitFor(t, c.Done(), "Done после Close")
	if !errors.Is(c.Err(), ErrClosed) {
		t.Fatalf("Err() = %v, want ErrClosed", c.Err())
	}
}

func TestSlowClientDisconnected(t *testing.T) {
	f := newFakeConn(true, true)
	var r reasons
	c := NewClient(f, 3, r.record)
	if !c.Send([]byte("m0")) {
		t.Fatal("Send(m0) = false")
	}
	waitFor(t, f.entered, "писатель начал Write(m0)")
	for i := range 3 {
		if !c.Send([]byte{'q', byte('1' + i)}) {
			t.Fatalf("Send #%d = false, want true: в очереди есть место", i+1)
		}
	}
	if c.Send([]byte("overflow")) {
		t.Fatal("Send при полной очереди = true, want false")
	}
	if !errors.Is(c.Err(), ErrSlowClient) {
		t.Fatalf("Err() = %v, want ErrSlowClient", c.Err())
	}
	waitFor(t, c.Done(), "Done после переполнения")
	if got := r.get(); len(got) != 1 || !errors.Is(got[0], ErrSlowClient) {
		t.Fatalf("onClose вызван с %v, want один раз с ErrSlowClient", got)
	}
	if w, closes := f.stats(); len(w) != 1 || closes != 1 {
		t.Fatalf("Write вызван %d раз, Close %d раз; want 1 и 1 — очередь после закрытия не пишется", len(w), closes)
	}
}

func TestSendNeverBlocks(t *testing.T) {
	f := newFakeConn(true, true)
	c := NewClient(f, 8, nil)
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 1000 {
			c.Send([]byte("update"))
		}
	}()
	waitFor(t, done, "1000 вызовов Send при зависшем Write")
	c.Close(nil)
	waitFor(t, c.Done(), "Done после Close")
}
