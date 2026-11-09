package task

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestAppendVarInt(t *testing.T) {
	tests := []struct {
		v    int32
		want []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{300, []byte{0xac, 0x02}},
		{25565, []byte{0xdd, 0xc7, 0x01}},
		{2147483647, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{-1, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
	}
	for _, tt := range tests {
		if got := AppendVarInt([]byte{0xaa}, tt.v); !bytes.Equal(got, append([]byte{0xaa}, tt.want...)) {
			t.Errorf("AppendVarInt(aa, %d) = % x, want aa % x", tt.v, got, tt.want)
		}
	}
}

func TestReadVarInt(t *testing.T) {
	tests := []struct {
		in      []byte
		want    int32
		wantErr error
	}{
		{[]byte{0x00}, 0, nil},
		{[]byte{0xac, 0x02}, 300, nil},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x0f}, -1, nil},
		{nil, 0, io.EOF},
		{[]byte{0x80}, 0, io.ErrUnexpectedEOF},
		{[]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 0, ErrVarIntTooLong},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x10}, 0, ErrVarIntOverflow},
		{[]byte{0x81, 0x00}, 0, ErrVarIntNonCanonical},
	}
	for _, tt := range tests {
		got, err := ReadVarInt(bytes.NewReader(tt.in))
		if tt.wantErr != nil {
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("ReadVarInt(% x): err = %v, want %v", tt.in, err, tt.wantErr)
			}
			continue
		}
		if err != nil || got != tt.want {
			t.Errorf("ReadVarInt(% x) = %d, %v; want %d", tt.in, got, err, tt.want)
		}
	}
}

func TestFramesGlued(t *testing.T) {
	var wire []byte
	wire = AppendFrame(wire, 0x01, []byte("ab"))
	wire = AppendFrame(wire, 0x02, nil)
	if want := []byte{0x03, 0x01, 'a', 'b', 0x01, 0x02}; !bytes.Equal(wire, want) {
		t.Fatalf("AppendFrame: % x, want % x", wire, want)
	}
	r := bytes.NewReader(wire)
	f, err := ReadFrame(r, 1024)
	if err != nil || f.ID != 0x01 || string(f.Data) != "ab" {
		t.Fatalf("первый кадр: %+v, %v", f, err)
	}
	f, err = ReadFrame(r, 1024)
	if err != nil || f.ID != 0x02 || len(f.Data) != 0 {
		t.Fatalf("второй кадр: %+v, %v", f, err)
	}
	if _, err = ReadFrame(r, 1024); err != io.EOF {
		t.Fatalf("после последнего кадра: err = %v, want io.EOF", err)
	}
}

func TestFrameLimits(t *testing.T) {
	ok := AppendFrame(nil, 0x01, make([]byte, 9)) // тело — 10 байт
	if _, err := ReadFrame(bytes.NewReader(ok), 10); err != nil {
		t.Fatalf("кадр ровно в лимит: %v", err)
	}
	if _, err := ReadFrame(bytes.NewReader(ok), 9); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("кадр больше лимита: err = %v, want ErrFrameTooLarge", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x00}), 10); !errors.Is(err, ErrBadLength) {
		t.Fatalf("длина 0: err = %v, want ErrBadLength", err)
	}
	if _, err := ReadFrame(bytes.NewReader([]byte{0x05, 0x01, 'a'}), 10); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("обрыв тела: err = %v, want io.ErrUnexpectedEOF", err)
	}
}

func TestDecodePacket(t *testing.T) {
	// Handshake: протокол 767, "localhost", порт 25565, next state 1.
	hs := []byte{0xff, 0x05, 0x09, 'l', 'o', 'c', 'a', 'l', 'h', 'o', 's', 't', 0x63, 0xdd, 0x01}
	p, err := DecodePacket(Frame{ID: 0x00, Data: hs})
	if want := (Handshake{767, "localhost", 25565, 1}); err != nil || p != want {
		t.Fatalf("Handshake: %#v, %v; want %#v", p, err, want)
	}
	p, err = DecodePacket(Frame{ID: 0x01, Data: append([]byte{0x05}, "hello"...)})
	if want := (Chat{"hello"}); err != nil || p != want {
		t.Fatalf("Chat: %#v, %v", p, err)
	}
	mv := []byte{0x3f, 0xf8, 0, 0, 0, 0, 0, 0, 0xc0, 0x24, 0, 0, 0, 0, 0, 0} // 1.5, -10
	p, err = DecodePacket(Frame{ID: 0x02, Data: mv})
	if want := (Move{1.5, -10}); err != nil || p != want {
		t.Fatalf("Move: %#v, %v", p, err)
	}
	if _, err = DecodePacket(Frame{ID: 0x01, Data: []byte{0x05, 'h', 'i'}}); !errors.Is(err, ErrShortPacket) {
		t.Fatalf("строка длиннее пакета: err = %v, want ErrShortPacket", err)
	}
	if _, err = DecodePacket(Frame{ID: 0x01, Data: []byte{0x01, 'h', 'i'}}); !errors.Is(err, ErrTrailingData) {
		t.Fatalf("лишние байты: err = %v, want ErrTrailingData", err)
	}
	if _, err = DecodePacket(Frame{ID: 0x07}); !errors.Is(err, ErrUnknownPacket) {
		t.Fatalf("id 0x07: err = %v, want ErrUnknownPacket", err)
	}
}

// Заготовка fuzz-теста. Без флага -fuzz прогоняется только seed-корпус;
// локально запустите `go test -fuzz=FuzzReadFrame` и допишите свои
// свойства (например, для DecodePacket).
func FuzzReadFrame(f *testing.F) {
	f.Add(AppendFrame(nil, 0x01, []byte{0x02, 'h', 'i'}))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0x07})
	f.Add([]byte{0x02, 0x80, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		r := bytes.NewReader(data)
		fr, err := ReadFrame(r, 64)
		if err != nil {
			return
		}
		used := len(data) - r.Len()
		if again := AppendFrame(nil, fr.ID, fr.Data); !bytes.Equal(again, data[:used]) {
			t.Fatalf("прочитано % x, записано % x", data[:used], again)
		}
		DecodePacket(fr) // не должен паниковать
	})
}
