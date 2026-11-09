package websocket

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAcceptKey(t *testing.T) {
	// Пример из RFC 6455, раздел 1.3.
	if got := acceptKey("dGhlIHNhbXBsZSBub25jZQ=="); got != "s3pPLMBiTxaQ9kYGzzhZRbK+xOo=" {
		t.Fatalf("got %s", got)
	}
}

// echoServer — сервер, который возвращает каждое сообщение.
func echoServer(t *testing.T, allowed string) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := Upgrade(w, r, func(o string) bool { return o == allowed })
		if err != nil {
			return
		}
		defer c.Close()
		c.MaxMessage = 1024
		for {
			op, data, err := c.ReadMessage()
			if err != nil {
				return
			}
			if err := c.WriteMessage(op, data); err != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return "ws" + strings.TrimPrefix(srv.URL, "http")
}

func dialT(t *testing.T, url, origin string) *Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	c, err := Dial(ctx, url, origin)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	return c
}

func TestEcho(t *testing.T) {
	url := echoServer(t, "http://ok")
	c := dialT(t, url, "http://ok")
	for _, msg := range []string{"", "привет", strings.Repeat("x", 200), strings.Repeat("y", 1000)} {
		if err := c.WriteMessage(OpText, []byte(msg)); err != nil {
			t.Fatal(err)
		}
		op, data, err := c.ReadMessage()
		if err != nil || op != OpText || string(data) != msg {
			t.Fatalf("echo %d bytes: op=%d len=%d err=%v", len(msg), op, len(data), err)
		}
	}
}

func TestOriginRejected(t *testing.T) {
	url := echoServer(t, "http://ok")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := Dial(ctx, url, "http://evil.example"); err == nil {
		t.Fatal("foreign origin must be rejected")
	}
	// Без Origin (не браузер) — пускаем.
	dialT(t, url, "")
}

// rawFrame — кадр клиента, собранный вручную (для нарушений протокола).
func rawFrame(fin bool, op byte, payload []byte, masked bool) []byte {
	b0 := op
	if fin {
		b0 |= 0x80
	}
	b := []byte{b0}
	mb := byte(0)
	if masked {
		mb = 0x80
	}
	switch n := len(payload); {
	case n < 126:
		b = append(b, mb|byte(n))
	case n < 1<<16:
		b = append(b, mb|126)
		b = binary.BigEndian.AppendUint16(b, uint16(n))
	default:
		b = append(b, mb|127)
		b = binary.BigEndian.AppendUint64(b, uint64(n))
	}
	if masked {
		mask := [4]byte{1, 2, 3, 4}
		b = append(b, mask[:]...)
		p := append([]byte(nil), payload...)
		unmask(p, mask, 0)
		return append(b, p...)
	}
	return append(b, payload...)
}

// serverPair — серверный Conn и «сырой» сокет клиента.
func serverPair(t *testing.T) (*Conn, net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	s := newConn(a, bufio.NewReader(a), true)
	s.MaxMessage = 100
	return s, b
}

func readClose(t *testing.T, client net.Conn) int {
	t.Helper()
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	h := make([]byte, 2)
	if _, err := io.ReadFull(client, h); err != nil {
		t.Fatal(err)
	}
	if h[0]&0x0F != OpClose {
		t.Fatalf("expected close frame, got op %d", h[0]&0x0F)
	}
	// net.Pipe синхронный: дочитываем кадр целиком, иначе сервер зависнет в Write.
	p := make([]byte, h[1]&0x7F)
	if _, err := io.ReadFull(client, p); err != nil || len(p) < 2 {
		t.Fatalf("close payload: %v", err)
	}
	return int(binary.BigEndian.Uint16(p))
}

func TestFragmentsAndPing(t *testing.T) {
	s, client := serverPair(t)
	go func() {
		client.Write(rawFrame(false, OpText, []byte("при"), true))
		client.Write(rawFrame(true, OpPing, []byte("p"), true)) // ping между фрагментами
		client.Write(rawFrame(true, OpContinuation, []byte("вет"), true))
	}()
	got := make(chan string, 1)
	go func() {
		_, data, err := s.ReadMessage()
		if err != nil {
			got <- err.Error()
			return
		}
		got <- string(data)
	}()
	// Сервер отвечает pong до завершения сообщения.
	_ = client.SetReadDeadline(time.Now().Add(2 * time.Second))
	h := make([]byte, 3)
	if _, err := client.Read(h); err != nil || h[0] != 0x80|OpPong || h[2] != 'p' {
		t.Fatalf("pong: %x %v", h, err)
	}
	if m := <-got; m != "привет" {
		t.Fatalf("message %q", m)
	}
}

func TestProtocolViolations(t *testing.T) {
	cases := []struct {
		name  string
		frame []byte
		err   error
		code  int
	}{
		{"unmasked client frame", rawFrame(true, OpText, []byte("x"), false), ErrProtocol, CloseProtocolError},
		{"too big", rawFrame(true, OpText, bytes.Repeat([]byte("x"), 200), true), ErrTooBig, CloseTooBig},
		{"bad utf-8", rawFrame(true, OpText, []byte{0xff, 0xfe}, true), ErrInvalidUTF8, CloseInvalidPayload},
		{"continuation first", rawFrame(true, OpContinuation, []byte("x"), true), ErrProtocol, CloseProtocolError},
		{"fragmented ping", rawFrame(false, OpPing, nil, true), ErrProtocol, CloseProtocolError},
		{"reserved opcode", rawFrame(true, 0x3, nil, true), ErrProtocol, CloseProtocolError},
		{"rsv bit", append([]byte{0x80 | 0x40 | OpText}, rawFrame(true, OpText, nil, true)[1:]...), ErrProtocol, CloseProtocolError},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, client := serverPair(t)
			go client.Write(c.frame)
			errc := make(chan error, 1)
			go func() { _, _, err := s.ReadMessage(); errc <- err }()
			if code := readClose(t, client); code != c.code {
				t.Fatalf("close code %d, want %d", code, c.code)
			}
			if err := <-errc; !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
		})
	}
}

func TestCloseHandshake(t *testing.T) {
	s, client := serverPair(t)
	go client.Write(rawFrame(true, OpClose, binary.BigEndian.AppendUint16(nil, CloseGoingAway), true))
	errc := make(chan error, 1)
	go func() { _, _, err := s.ReadMessage(); errc <- err }()
	if code := readClose(t, client); code != CloseGoingAway {
		t.Fatalf("echoed close code %d", code)
	}
	var ce *CloseError
	if err := <-errc; !errors.As(err, &ce) || ce.Code != CloseGoingAway {
		t.Fatalf("err = %v", err)
	}
	if err := s.WriteMessage(OpText, []byte("late")); err == nil {
		t.Fatal("write after close must fail")
	}
}

// FuzzReadMessage — произвольный поток байт от клиента: без паник и
// без сообщений больше предела.
func FuzzReadMessage(f *testing.F) {
	f.Add(rawFrame(true, OpText, []byte("hello"), true))
	f.Add(append(rawFrame(false, OpText, []byte("a"), true), rawFrame(true, OpContinuation, []byte("b"), true)...))
	f.Add(rawFrame(true, OpPing, []byte("x"), true))
	f.Add([]byte{0x81, 0xFF, 0, 0, 0, 0, 0, 0, 0x10, 0})
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bufio.NewReader(bytes.NewReader(data))
		c := newConn(discardConn{}, r, true)
		c.MaxMessage = 512
		for i := 0; i < 16; i++ {
			_, msg, err := c.ReadMessage()
			if err != nil {
				return
			}
			if len(msg) > 512 {
				t.Fatalf("message %d > limit", len(msg))
			}
		}
	})
}

// discardConn — net.Conn, в который можно писать (ответы pong/close).
type discardConn struct{ net.Conn }

func (discardConn) Write(b []byte) (int, error)      { return len(b), nil }
func (discardConn) SetReadDeadline(time.Time) error  { return nil }
func (discardConn) SetWriteDeadline(time.Time) error { return nil }
func (discardConn) Close() error                     { return nil }

func TestInvalidCloseFromClient(t *testing.T) {
	cases := []struct {
		name    string
		payload []byte
		code    int
		err     error
	}{
		{"reserved 1005", binary.BigEndian.AppendUint16(nil, 1005), CloseProtocolError, ErrProtocol},
		{"below 1000", binary.BigEndian.AppendUint16(nil, 999), CloseProtocolError, ErrProtocol},
		{"unassigned 2000", binary.BigEndian.AppendUint16(nil, 2000), CloseProtocolError, ErrProtocol},
		{"bad utf-8 reason", append(binary.BigEndian.AppendUint16(nil, CloseNormal), 0xff), CloseInvalidPayload, ErrInvalidUTF8},
		{"private 4000 ok", binary.BigEndian.AppendUint16(nil, 4000), 4000, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s, client := serverPair(t)
			go client.Write(rawFrame(true, OpClose, c.payload, true))
			errc := make(chan error, 1)
			go func() { _, _, err := s.ReadMessage(); errc <- err }()
			if code := readClose(t, client); code != c.code {
				t.Fatalf("close code %d, want %d", code, c.code)
			}
			err := <-errc
			if c.err == nil {
				var ce *CloseError
				if !errors.As(err, &ce) {
					t.Fatalf("err = %v", err)
				}
			} else if !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
		})
	}
}

func TestFrameLimit(t *testing.T) {
	s, client := serverPair(t)
	n := 0
	s.AllowFrame = func() bool { n++; return n <= 3 }
	go func() {
		for i := 0; i < 10; i++ {
			if _, err := client.Write(rawFrame(true, OpPong, nil, true)); err != nil {
				return
			}
		}
	}()
	errc := make(chan error, 1)
	go func() { _, _, err := s.ReadMessage(); errc <- err }()
	if code := readClose(t, client); code != ClosePolicy {
		t.Fatalf("close code %d", code)
	}
	if err := <-errc; !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v", err)
	}
}

// TestPongWriteTimeout — клиент шлёт ping и не читает: запись pong
// упирается в дедлайн, ReadMessage возвращает ошибку, а не висит вечно.
func TestPongWriteTimeout(t *testing.T) {
	s, client := serverPair(t)
	s.WriteTimeout = 100 * time.Millisecond
	go client.Write(rawFrame(true, OpPing, []byte("x"), true)) // net.Pipe: pong некому прочитать
	errc := make(chan error, 1)
	go func() { _, _, err := s.ReadMessage(); errc <- err }()
	select {
	case err := <-errc:
		if !errors.Is(err, os.ErrDeadlineExceeded) {
			t.Fatalf("err = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("ReadMessage hangs on pong write")
	}
}
