package task

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestUncompressedBelowThreshold(t *testing.T) {
	packet := []byte{0x26, 0, 0, 0, 0, 0, 0, 0, 7} // keep-alive play, id 0x26
	got, err := AppendCompressed([]byte{0xaa}, packet, 256)
	if err != nil {
		t.Fatal(err)
	}
	want := append([]byte{0xaa, 0x0a, 0x00}, packet...)
	if !bytes.Equal(got, want) {
		t.Fatalf("кадр % x, want % x", got, want)
	}
	back, err := ReadCompressed(bytes.NewReader(got[1:]), 256, 1<<20)
	if err != nil || !bytes.Equal(back, packet) {
		t.Fatalf("ReadCompressed: % x, %v", back, err)
	}
}

func TestCompressedAboveThreshold(t *testing.T) {
	packet := append([]byte{0x01}, strings.Repeat("Добро пожаловать! ", 50)...)
	frame, err := AppendCompressed(nil, packet, 256)
	if err != nil {
		t.Fatal(err)
	}
	if len(frame) >= len(packet) {
		t.Fatalf("кадр %d байт не короче пакета %d", len(frame), len(packet))
	}
	// Разбираем формат вручную: длина кадра, длина данных, zlib.
	r := bytes.NewReader(frame)
	n, _ := tReadVarInt(r)
	if int(n) != r.Len() {
		t.Fatalf("Packet Length %d, а байт после неё %d", n, r.Len())
	}
	dl, _ := tReadVarInt(r)
	if int(dl) != len(packet) {
		t.Fatalf("Data Length %d, want %d", dl, len(packet))
	}
	zr, err := zlib.NewReader(r)
	if err != nil {
		t.Fatalf("zlib: %v", err)
	}
	raw, err := io.ReadAll(zr)
	if err != nil || !bytes.Equal(raw, packet) {
		t.Fatalf("распаковка стандартным zlib: %v", err)
	}
	back, err := ReadCompressed(bytes.NewReader(frame), 256, 1<<20)
	if err != nil || !bytes.Equal(back, packet) {
		t.Fatalf("ReadCompressed: %v", err)
	}
}

func TestReadLimits(t *testing.T) {
	packet := bytes.Repeat([]byte{'a'}, 1000)
	frame, _ := AppendCompressed(nil, packet, 256)
	if _, err := ReadCompressed(bytes.NewReader(frame), 256, 999); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("длина данных больше maxData: err = %v, want ErrTooLarge", err)
	}
	if _, err := ReadCompressed(bytes.NewReader(frame), 1001, 1<<20); !errors.Is(err, ErrBelowThreshold) {
		t.Fatalf("сжатый пакет меньше порога: err = %v, want ErrBelowThreshold", err)
	}
	if _, err := ReadCompressed(bytes.NewReader(nil), 256, 1<<20); err != io.EOF {
		t.Fatalf("пустой поток: err = %v, want io.EOF", err)
	}
}

// Вспомогательные VarInt для тестов (без проверок строгости).
func tAppendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for ; u >= 0x80; u >>= 7 {
		b = append(b, byte(u)|0x80)
	}
	return append(b, byte(u))
}

func tReadVarInt(r io.ByteReader) (int32, error) {
	var u uint32
	for i := range 5 {
		c, err := r.ReadByte()
		if err != nil {
			return 0, err
		}
		u |= uint32(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return int32(u), nil
		}
	}
	return 0, errors.New("varint too long")
}
