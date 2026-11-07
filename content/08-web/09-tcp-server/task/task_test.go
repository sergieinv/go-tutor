package task

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	for _, msg := range []string{"hello", "", "мир"} {
		if err := WriteFrame(&buf, []byte(msg), 64); err != nil {
			t.Fatalf("WriteFrame(%q) = %v", msg, err)
		}
	}
	for _, want := range []string{"hello", "", "мир"} {
		got, err := ReadFrame(&buf, 64)
		if err != nil || string(got) != want {
			t.Fatalf("ReadFrame = (%q, %v), want %q", got, err, want)
		}
	}
	if _, err := ReadFrame(&buf, 64); err != io.EOF {
		t.Fatalf("ReadFrame в конце потока = %v, want io.EOF", err)
	}
}

func TestWireFormat(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, []byte("abc"), 10); err != nil {
		t.Fatal(err)
	}
	want := []byte{0, 0, 0, 3, 'a', 'b', 'c'}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("байты кадра = %v, want %v", buf.Bytes(), want)
	}
}

func TestTooLarge(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteFrame(&buf, make([]byte, 11), 10); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("WriteFrame 11 байт при max 10 = %v, want ErrFrameTooLarge", err)
	}
	r := bytes.NewReader([]byte{0, 0, 1, 0}) // заголовок: 256 байт
	if _, err := ReadFrame(r, 255); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ReadFrame 256 байт при max 255 = %v, want ErrFrameTooLarge", err)
	}
}

func TestTruncated(t *testing.T) {
	r := bytes.NewReader([]byte{0, 0, 0, 5, 'a', 'b'})
	if _, err := ReadFrame(r, 64); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("обрезанный кадр: %v, want io.ErrUnexpectedEOF", err)
	}
}
