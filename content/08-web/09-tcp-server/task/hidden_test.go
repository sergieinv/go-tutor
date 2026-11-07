package task

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"math/rand/v2"
	"net"
	"testing"
	"testing/iotest"
	"time"
)

// countingWriter считает вызовы Write.
type countingWriter struct {
	bytes.Buffer
	calls int
}

func (w *countingWriter) Write(p []byte) (int, error) {
	w.calls++
	return w.Buffer.Write(p)
}

func TestWriteSingleCall(t *testing.T) {
	var w countingWriter
	if err := WriteFrame(&w, []byte("payload"), 100); err != nil {
		t.Fatal(err)
	}
	if w.calls != 1 {
		t.Fatalf("WriteFrame вызвал Write %d раз, want 1: заголовок и данные — одним буфером", w.calls)
	}
}

func TestWriteTooLargeWritesNothing(t *testing.T) {
	var w countingWriter
	err := WriteFrame(&w, []byte("12345"), 4)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame = %v, want ErrFrameTooLarge", err)
	}
	if w.calls != 0 || w.Len() != 0 {
		t.Fatalf("при ошибке размера записано %d байт за %d вызовов, want ничего", w.Len(), w.calls)
	}
	// Ровно max — можно, max 0 разрешает только пустые кадры.
	if err := WriteFrame(&w, []byte("1234"), 4); err != nil {
		t.Fatalf("WriteFrame ровно max = %v", err)
	}
	if err := WriteFrame(&w, nil, 0); err != nil {
		t.Fatalf("WriteFrame(nil, max 0) = %v", err)
	}
	if err := WriteFrame(&w, []byte{1}, 0); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame 1 байт при max 0 = %v, want ErrFrameTooLarge", err)
	}
}

type failWriter struct{ err error }

func (w failWriter) Write([]byte) (int, error) { return 0, w.err }

func TestWriteError(t *testing.T) {
	boom := errors.New("connection reset")
	if err := WriteFrame(failWriter{boom}, []byte("x"), 10); !errors.Is(err, boom) {
		t.Fatalf("WriteFrame = %v, want ошибку писателя", err)
	}
}

func TestReadOneByteAtATime(t *testing.T) {
	// TCP отдаёт данные кусками произвольного размера.
	var buf bytes.Buffer
	WriteFrame(&buf, []byte("первый"), 100)
	WriteFrame(&buf, []byte("второй"), 100)
	r := iotest.OneByteReader(&buf)
	for _, want := range []string{"первый", "второй"} {
		got, err := ReadFrame(r, 100)
		if err != nil || string(got) != want {
			t.Fatalf("ReadFrame по байту = (%q, %v), want %q: используйте io.ReadFull", got, err, want)
		}
	}
	if _, err := ReadFrame(r, 100); err != io.EOF {
		t.Fatalf("конец потока = %v, want ровно io.EOF", err)
	}
}

func TestReadDataWithEOF(t *testing.T) {
	var buf bytes.Buffer
	WriteFrame(&buf, []byte("last"), 100)
	got, err := ReadFrame(iotest.DataErrReader(&buf), 100)
	if err != nil || string(got) != "last" {
		t.Fatalf("ReadFrame = (%q, %v), want \"last\"", got, err)
	}
}

func TestReadDoesNotOverread(t *testing.T) {
	data := []byte{0, 0, 0, 2, 'h', 'i', 0, 0, 0, 1, '!'}
	r := bytes.NewReader(data)
	if got, err := ReadFrame(r, 10); err != nil || string(got) != "hi" {
		t.Fatalf("ReadFrame = (%q, %v)", got, err)
	}
	if r.Len() != 5 {
		t.Fatalf("после первого кадра в потоке осталось %d байт, want 5: ReadFrame не должен читать лишнее (без bufio внутри)", r.Len())
	}
}

func TestReadTooLargeStopsAfterHeader(t *testing.T) {
	data := append([]byte{0xFF, 0xFF, 0xFF, 0xFF}, bytes.Repeat([]byte{'x'}, 100)...)
	r := bytes.NewReader(data)
	got, err := ReadFrame(r, 1<<20)
	if !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ReadFrame(длина 4 ГБ) = %v, want ErrFrameTooLarge", err)
	}
	if got != nil {
		t.Errorf("при ошибке ReadFrame вернул данные %q, want nil", got)
	}
	if r.Len() != 100 {
		t.Errorf("после слишком длинного заголовка прочитано %d байт данных, want 0", 100-r.Len())
	}
	// Ровно max — можно.
	if got, err := ReadFrame(bytes.NewReader([]byte{0, 0, 0, 1, 'z'}), 1); err != nil || string(got) != "z" {
		t.Fatalf("кадр ровно max: (%q, %v)", got, err)
	}
}

func TestReadEmptyFrame(t *testing.T) {
	got, err := ReadFrame(bytes.NewReader([]byte{0, 0, 0, 0}), 0)
	if err != nil {
		t.Fatalf("пустой кадр: %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("пустой кадр = %#v, want []byte{} (не nil)", got)
	}
}

func TestReadTruncatedHeaderAndPayload(t *testing.T) {
	cases := [][]byte{
		{0},
		{0, 0, 0},
		{0, 0, 0, 1},
		{0, 0, 0, 4, 'a', 'b', 'c'},
	}
	for _, c := range cases {
		got, err := ReadFrame(bytes.NewReader(c), 100)
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("ReadFrame(%v) = %v, want io.ErrUnexpectedEOF", c, err)
		}
		if got != nil {
			t.Errorf("ReadFrame(%v) вернул данные %q при ошибке", c, got)
		}
	}
	if _, err := ReadFrame(bytes.NewReader(nil), 100); err != io.EOF {
		t.Errorf("пустой поток = %v, want ровно io.EOF", err)
	}
}

func TestReadError(t *testing.T) {
	boom := errors.New("read: connection reset")
	if _, err := ReadFrame(iotest.ErrReader(boom), 10); !errors.Is(err, boom) {
		t.Fatalf("ReadFrame = %v, want ошибку читателя", err)
	}
	r := io.MultiReader(bytes.NewReader([]byte{0, 0, 0, 3, 'a'}), iotest.ErrReader(boom))
	if _, err := ReadFrame(r, 10); !errors.Is(err, boom) {
		t.Fatalf("ReadFrame с ошибкой посреди данных = %v, want ошибку читателя", err)
	}
}

func TestOverPipe(t *testing.T) {
	// net.Pipe — синхронное соединение в памяти: каждый Write ждёт,
	// пока другая сторона всё прочитает.
	client, server := net.Pipe()
	defer server.Close()
	rnd := rand.New(rand.NewPCG(1, 2))
	msgs := make([][]byte, 50)
	for i := range msgs {
		msgs[i] = make([]byte, rnd.IntN(3000))
		for j := range msgs[i] {
			msgs[i][j] = byte(rnd.Uint32())
		}
	}
	errc := make(chan error, 1)
	go func() {
		defer client.Close()
		for _, m := range msgs {
			if err := WriteFrame(client, m, 4096); err != nil {
				errc <- err
				return
			}
		}
		errc <- nil
	}()
	server.SetDeadline(time.Now().Add(5 * time.Second))
	for i, want := range msgs {
		got, err := ReadFrame(server, 4096)
		if err != nil || !bytes.Equal(got, want) {
			t.Fatalf("кадр %d: (%d байт, %v), want %d байт", i, len(got), err, len(want))
		}
	}
	if err := <-errc; err != nil {
		t.Fatalf("WriteFrame: %v", err)
	}
	if _, err := ReadFrame(server, 4096); err != io.EOF {
		t.Fatalf("после закрытия клиента = %v, want io.EOF", err)
	}
}

func TestOverTCP(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		// Эхо-сервер на кадрах: отвечает тем же кадром в верхнем регистре.
		for {
			p, err := ReadFrame(conn, 1024)
			if err != nil {
				return
			}
			WriteFrame(conn, bytes.ToUpper(p), 1024)
		}
	}()
	conn, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	conn.SetDeadline(time.Now().Add(5 * time.Second))
	for i := range 20 {
		msg := fmt.Sprintf("message %d %s", i, bytes.Repeat([]byte("ab"), i*20))
		if err := WriteFrame(conn, []byte(msg), 1024); err != nil {
			t.Fatal(err)
		}
		got, err := ReadFrame(conn, 1024)
		if err != nil || string(got) != string(bytes.ToUpper([]byte(msg))) {
			t.Fatalf("ответ %d: (%q, %v)", i, got, err)
		}
	}
}
