package task

import (
	"go/ast"
	"go/parser"
	"go/token"
	"sync/atomic"
	"testing"
	"time"
)

// timedOut — один waitUntil уже не дождался: дальше ждём недолго,
// чтобы незаконченное решение не упиралось в лимит времени песочницы.
var timedOut atomic.Bool

// waitUntil ждёт, пока cond станет истинным; щедрый таймаут вместо sleep.
func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	limit := 2 * time.Second
	if timedOut.Load() {
		limit = 50 * time.Millisecond
	}
	deadline := time.Now().Add(limit)
	for !cond() {
		if time.Now().After(deadline) {
			timedOut.Store(true)
			t.Fatalf("не дождались: %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

// panics вызывает f и возвращает значение паники (nil — паники не было).
func panics(f func()) (r any) {
	defer func() { r = recover() }()
	f()
	return nil
}

func TestBufferedFIFO(t *testing.T) {
	c := New[int](3)
	c.Send(1)
	c.Send(2)
	c.Send(3)
	if c.Len() != 3 || c.Cap() != 3 {
		t.Fatalf("Len, Cap = %d, %d, want 3, 3", c.Len(), c.Cap())
	}
	for want := 1; want <= 3; want++ {
		if v, ok := c.Recv(); v != want || !ok {
			t.Fatalf("Recv = (%d, %v), want (%d, true)", v, ok, want)
		}
	}
	if c.Len() != 0 {
		t.Errorf("Len после чтения = %d, want 0", c.Len())
	}
}

func TestRecvFromClosed(t *testing.T) {
	c := New[string](2)
	c.Send("a")
	c.Close()
	if v, ok := c.Recv(); v != "a" || !ok {
		t.Fatalf("Recv после Close = (%q, %v), want (\"a\", true): буфер дочитывается", v, ok)
	}
	for range 2 {
		if v, ok := c.Recv(); v != "" || ok {
			t.Fatalf("Recv из закрытого пустого = (%q, %v), want (\"\", false)", v, ok)
		}
	}
}

func TestSendOnClosedPanics(t *testing.T) {
	c := New[int](1)
	c.Close()
	r := panics(func() { c.Send(1) })
	if r != "send on closed channel" {
		t.Fatalf("Send в закрытый: паника %v, want \"send on closed channel\"", r)
	}
}

func TestUnbufferedRendezvous(t *testing.T) {
	c := New[int](0)
	var sent atomic.Bool
	go func() {
		c.Send(42)
		sent.Store(true)
	}()
	waitUntil(t, "отправитель встал в очередь (Waiting senders == 1)", func() bool {
		s, _ := c.Waiting()
		return s == 1
	})
	time.Sleep(20 * time.Millisecond)
	if sent.Load() {
		t.Fatal("Send в небуферизованный канал вернулся без получателя")
	}
	if v, ok := c.Recv(); v != 42 || !ok {
		t.Fatalf("Recv = (%d, %v), want (42, true)", v, ok)
	}
	waitUntil(t, "Send вернулся после Recv", sent.Load)
	if c.Len() != 0 || c.Cap() != 0 {
		t.Errorf("Len, Cap = %d, %d, want 0, 0", c.Len(), c.Cap())
	}
}

// Встроенный chan сделал бы задачу тривиальной — проверяем исходник.
func TestNoBuiltinChannels(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "task.go", nil, 0)
	if err != nil {
		t.Fatalf("не разобрать task.go: %v", err)
	}
	ast.Inspect(f, func(n ast.Node) bool {
		switch n := n.(type) {
		case *ast.ChanType:
			t.Errorf("task.go использует тип chan — нельзя, реализуйте на sync.Mutex и sync.Cond")
		case *ast.SendStmt:
			t.Errorf("task.go использует отправку в канал — нельзя")
		case *ast.SelectStmt:
			t.Errorf("task.go использует select — нельзя")
		case *ast.UnaryExpr:
			if n.Op == token.ARROW {
				t.Errorf("task.go использует получение из канала — нельзя")
			}
		}
		return true
	})
}
