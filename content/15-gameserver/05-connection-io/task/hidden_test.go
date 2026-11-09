package task

import (
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"
)

// Порядок и содержимое на настоящем соединении net.Pipe:
// много сообщений, читатель медленнее писателя.
func TestHiddenPipeOrder(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	c := NewClient(server, 64, nil)
	got := make(chan string, 100)
	go func() {
		defer close(got)
		buf := make([]byte, 6)
		for {
			if _, err := io.ReadFull(client, buf); err != nil {
				return
			}
			got <- string(buf)
		}
	}()
	for i := range 50 {
		if !c.Send(fmt.Appendf(nil, "msg%03d", i)) {
			t.Fatalf("Send #%d = false: очередь 64 не должна переполниться", i)
		}
	}
	for i := range 50 {
		select {
		case s := <-got:
			if want := fmt.Sprintf("msg%03d", i); s != want {
				t.Fatalf("сообщение %d = %q, want %q — порядок FIFO", i, s, want)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("сообщение %d не пришло", i)
		}
	}
	c.Close(nil)
	waitFor(t, c.Done(), "Done")
}

// Ошибка Write — причина отключения; соединение закрыто ровно один раз.
func TestHiddenWriteError(t *testing.T) {
	f := newFakeConn(false, false)
	boom := errors.New("connection reset by peer")
	f.err = boom
	var r reasons
	c := NewClient(f, 4, r.record)
	c.Send([]byte("a"))
	waitFor(t, c.Done(), "Done после ошибки записи")
	if !errors.Is(c.Err(), boom) {
		t.Fatalf("Err() = %v, want ошибку записи", c.Err())
	}
	if got := r.get(); len(got) != 1 || !errors.Is(got[0], boom) {
		t.Fatalf("onClose вызван с %v, want один раз с ошибкой записи", got)
	}
	if c.Send([]byte("b")) {
		t.Fatal("Send после отключения = true")
	}
	if w, closes := f.stats(); len(w) != 1 || closes != 1 {
		t.Fatalf("Write %d, Close %d; want 1 и 1", len(w), closes)
	}
	if got := r.get(); len(got) != 1 {
		t.Fatalf("onClose вызван %d раз, want 1", len(got))
	}
}

// Close(nil) — ErrClosed; повторный Close не меняет причину.
func TestHiddenCloseIdempotent(t *testing.T) {
	f := newFakeConn(false, false)
	var r reasons
	c := NewClient(f, 2, r.record)
	first := errors.New("kicked: flying is not enabled")
	c.Close(first)
	c.Close(errors.New("second"))
	c.Close(nil)
	waitFor(t, c.Done(), "Done")
	if c.Err() != first {
		t.Fatalf("Err() = %v, want причину первого Close", c.Err())
	}
	if got := r.get(); len(got) != 1 || got[0] != first {
		t.Fatalf("onClose вызван с %v, want один раз с первой причиной", got)
	}
	if _, closes := f.stats(); closes != 1 {
		t.Fatalf("conn.Close вызван %d раз, want 1", closes)
	}

	c2 := NewClient(newFakeConn(false, false), 2, nil)
	c2.Close(nil)
	waitFor(t, c2.Done(), "Done")
	if !errors.Is(c2.Err(), ErrClosed) {
		t.Fatalf("Close(nil): Err() = %v, want ErrClosed", c2.Err())
	}
}

// Закрытие простаивающего клиента: писатель ждал очередь и вышел,
// ни одного Write.
func TestHiddenCloseIdle(t *testing.T) {
	f := newFakeConn(false, false)
	c := NewClient(f, 4, nil) // onClose == nil — допустимо
	if c.Err() != nil {
		t.Fatalf("Err() = %v у открытого клиента", c.Err())
	}
	c.Close(nil)
	waitFor(t, c.Done(), "Done у простаивающего клиента")
	if w, _ := f.stats(); len(w) != 0 {
		t.Fatalf("Write вызван %d раз, want 0", len(w))
	}
	if c.Send([]byte("late")) {
		t.Fatal("Send после Close = true")
	}
	time.Sleep(10 * time.Millisecond)
	if w, _ := f.stats(); len(w) != 0 {
		t.Fatalf("после Close записано %d сообщений, want 0", len(w))
	}
}

// Close не ждёт писателя: Write висит и на Close не реагирует.
func TestHiddenCloseDoesNotWaitForWriter(t *testing.T) {
	f := newFakeConn(true, false)
	var r reasons
	c := NewClient(f, 2, r.record)
	c.Send([]byte("stuck"))
	waitFor(t, f.entered, "Write начался")
	closed := make(chan struct{})
	go func() { c.Close(errors.New("shutdown")); close(closed) }()
	waitFor(t, closed, "Close вернулся, пока Write висит")
	if c.Send([]byte("x")) {
		t.Fatal("Send после Close = true")
	}
	select {
	case <-c.Done():
		t.Fatal("Done закрыт, хотя писатель ещё в Write")
	default:
	}
	if len(r.get()) != 0 {
		t.Fatal("onClose вызван, пока писатель ещё в Write")
	}
	close(f.block)
	waitFor(t, c.Done(), "Done после того, как Write вернулся")
	if got := r.get(); len(got) != 1 {
		t.Fatalf("onClose вызван %d раз, want 1", len(got))
	}
	if w, _ := f.stats(); len(w) != 1 {
		t.Fatalf("Write вызван %d раз, want 1: после Close писатель не берёт новых сообщений", len(w))
	}
}

// queue < 1 считается как 1.
func TestHiddenQueueMinimum(t *testing.T) {
	for _, q := range []int{0, -5} {
		f := newFakeConn(true, true)
		c := NewClient(f, q, nil)
		c.Send([]byte("m0"))
		waitFor(t, f.entered, "Write(m0)")
		if !c.Send([]byte("m1")) {
			t.Fatalf("queue=%d: второй Send = false, want true (очередь 1)", q)
		}
		if c.Send([]byte("m2")) {
			t.Fatalf("queue=%d: третий Send = true, want false", q)
		}
		if !errors.Is(c.Err(), ErrSlowClient) {
			t.Fatalf("queue=%d: Err() = %v", q, c.Err())
		}
		waitFor(t, c.Done(), "Done")
	}
}

// Send и Close одновременно из многих горутин: без паники,
// onClose ровно один раз.
func TestHiddenConcurrentSendClose(t *testing.T) {
	for range 50 {
		f := newFakeConn(false, false)
		var r reasons
		c := NewClient(f, 4, r.record)
		var wg sync.WaitGroup
		for i := range 8 {
			wg.Go(func() {
				for j := range 50 {
					c.Send([]byte{byte(i), byte(j)})
					if i == 0 && j == 25 {
						c.Close(nil)
					}
				}
			})
		}
		wg.Go(func() { c.Close(errors.New("other")) })
		wg.Wait()
		waitFor(t, c.Done(), "Done")
		if got := r.get(); len(got) != 1 {
			t.Fatalf("onClose вызван %d раз, want 1", len(got))
		}
		if _, closes := f.stats(); closes != 1 {
			t.Fatalf("conn.Close вызван %d раз, want 1", closes)
		}
	}
}

// После закрытия всех клиентов горутины не остаются.
func TestHiddenNoGoroutineLeak(t *testing.T) {
	before := runtime.NumGoroutine()
	var clients []*Client
	for i := range 40 {
		f := newFakeConn(i%2 == 0, true)
		c := NewClient(f, 2, nil)
		c.Send([]byte("x"))
		clients = append(clients, c)
	}
	for _, c := range clients {
		c.Close(nil)
	}
	for _, c := range clients {
		waitFor(t, c.Done(), "Done")
	}
	for range 200 {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("горутин было %d, стало %d — писатели не завершились", before, runtime.NumGoroutine())
}

// Переполнение на net.Pipe: клиент не читает вовсе, тик не ждёт.
func TestHiddenPipeSlowReader(t *testing.T) {
	server, client := net.Pipe()
	defer client.Close()
	var r reasons
	c := NewClient(server, 5, r.record)
	sent := 0
	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 100 {
			if !c.Send([]byte("update")) {
				return
			}
			sent++
		}
	}()
	waitFor(t, done, "Send при нечитающем клиенте")
	if sent > 6 {
		t.Fatalf("принято %d сообщений, want не больше 6 (очередь 5 + одно в Write)", sent)
	}
	waitFor(t, c.Done(), "Done")
	if got := r.get(); len(got) != 1 || !errors.Is(got[0], ErrSlowClient) {
		t.Fatalf("onClose вызван с %v, want ErrSlowClient", got)
	}
}
