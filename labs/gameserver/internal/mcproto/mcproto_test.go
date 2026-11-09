package mcproto

import (
	"bytes"
	"compress/zlib"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
)

func TestVarInt(t *testing.T) {
	// Примеры из описания протокола.
	cases := []struct {
		v   int32
		enc []byte
	}{
		{0, []byte{0x00}},
		{1, []byte{0x01}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{255, []byte{0xff, 0x01}},
		{25565, []byte{0xdd, 0xc7, 0x01}},
		{2097151, []byte{0xff, 0xff, 0x7f}},
		{math.MaxInt32, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{-1, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
		{math.MinInt32, []byte{0x80, 0x80, 0x80, 0x80, 0x08}},
	}
	for _, c := range cases {
		got := AppendVarInt(nil, c.v)
		if !bytes.Equal(got, c.enc) {
			t.Errorf("AppendVarInt(%d) = %x, want %x", c.v, got, c.enc)
		}
		if VarIntSize(c.v) != len(c.enc) {
			t.Errorf("VarIntSize(%d) = %d", c.v, VarIntSize(c.v))
		}
		d := NewDecoder(c.enc)
		if v := d.VarInt(); v != c.v || d.Finish() != nil {
			t.Errorf("decode %x = %d, %v", c.enc, v, d.Err())
		}
	}
	d := NewDecoder([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x01})
	if d.VarInt(); !errors.Is(d.Err(), ErrVarIntTooBig) {
		t.Fatalf("6-byte varint: %v", d.Err())
	}
}

func TestPosition(t *testing.T) {
	for _, p := range [][3]int32{{0, 0, 0}, {18357644, 831, -20882616}, {-1, -64, -1}, {33554431, 2047, -33554432}} {
		b := AppendPosition(nil, p[0], p[1], p[2])
		d := NewDecoder(b)
		x, y, z := d.Position()
		if [3]int32{x, y, z} != p {
			t.Errorf("roundtrip %v = %v", p, [3]int32{x, y, z})
		}
	}
}

func TestStringLimits(t *testing.T) {
	b := AppendString(nil, "привет")
	d := NewDecoder(b)
	if s := d.String(6); s != "привет" || d.Err() != nil {
		t.Fatalf("got %q %v", s, d.Err())
	}
	d = NewDecoder(b)
	if d.String(5); !errors.Is(d.Err(), ErrStringTooBig) {
		t.Fatalf("limit in chars: %v", d.Err())
	}
	d = NewDecoder(AppendString(nil, "\xff\xfe"))
	if d.String(10); !errors.Is(d.Err(), ErrBadUTF8) {
		t.Fatalf("invalid utf-8: %v", d.Err())
	}
	// Длина больше, чем данных.
	d = NewDecoder([]byte{0x10, 'a'})
	if d.String(100); !errors.Is(d.Err(), ErrShort) {
		t.Fatalf("short: %v", d.Err())
	}
}

func TestOfflineUUID(t *testing.T) {
	// Известное значение: UUID offline-игрока Notch на ванильном сервере.
	if got := FormatUUID(OfflineUUID("Notch")); got != "b50ad385-829d-3141-a216-7e7d7539ba7f" {
		t.Fatalf("got %s", got)
	}
}

func TestFrameRoundtrip(t *testing.T) {
	for _, threshold := range []int{-1, 0, 64} {
		pw := NewPacketWriter(threshold)
		var stream []byte
		small := pw.Begin(0x1A)
		small = AppendDouble(small, 1.5)
		stream = pw.Frame(stream, small)
		big := pw.Begin(0x27)
		big = append(big, bytes.Repeat([]byte{0xFF}, 5000)...)
		stream = pw.Frame(stream, big)

		fr := NewFrameReader(bytes.NewReader(stream), 1<<16)
		fr.MaxData = 1 << 16
		fr.SetCompression(threshold)
		id, p, err := fr.Next()
		if err != nil || id != 0x1A || len(p) != 8 {
			t.Fatalf("threshold %d small: id=%x len=%d err=%v", threshold, id, len(p), err)
		}
		id, p, err = fr.Next()
		if err != nil || id != 0x27 || len(p) != 5000 {
			t.Fatalf("threshold %d big: id=%x len=%d err=%v", threshold, id, len(p), err)
		}
		if threshold >= 0 && len(stream) > 1000 {
			t.Fatalf("threshold %d: 5000 bytes of 0xFF must compress, stream %d", threshold, len(stream))
		}
		if _, _, err := fr.Next(); err != io.EOF {
			t.Fatalf("want EOF, got %v", err)
		}
	}
}

func TestFrameLimits(t *testing.T) {
	// Кадр длиннее предела отклоняется по заголовку, без чтения тела.
	frame := AppendVarInt(nil, 100000)
	fr := NewFrameReader(bytes.NewReader(frame), 1024)
	if _, _, err := fr.Next(); !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("big frame: %v", err)
	}
	// Длина кадра в 4 байта VarInt — нарушение протокола (максимум 3).
	fr = NewFrameReader(bytes.NewReader([]byte{0xff, 0xff, 0xff, 0x01}), 1024)
	if _, _, err := fr.Next(); !errors.Is(err, ErrVarIntTooBig) {
		t.Fatalf("4-byte length: %v", err)
	}
}

// zipBomb — маленький кадр, распаковывающийся в мегабайты.
func zipBomb(declared, real int) []byte {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	zw.Write(AppendVarInt(nil, 0x06))
	zw.Write(make([]byte, real))
	zw.Close()
	body := AppendVarInt(nil, int32(declared))
	body = append(body, z.Bytes()...)
	return append(AppendVarInt(nil, int32(len(body))), body...)
}

func TestDecompressionLimits(t *testing.T) {
	// Заявлено больше предела — отказ до распаковки.
	fr := NewFrameReader(bytes.NewReader(zipBomb(8<<20, 8<<20)), 1<<16)
	fr.MaxData = 1 << 16
	fr.SetCompression(256)
	if _, _, err := fr.Next(); !errors.Is(err, ErrFrameTooBig) {
		t.Fatalf("declared too big: %v", err)
	}
	// Заявлено мало, на деле больше — тоже отказ (не распаковываем сверх заявленного).
	fr = NewFrameReader(bytes.NewReader(zipBomb(1000, 8<<20)), 1<<16)
	fr.MaxData = 1 << 16
	fr.SetCompression(256)
	if _, _, err := fr.Next(); !errors.Is(err, ErrBadCompressed) {
		t.Fatalf("lying length: %v", err)
	}
	// Сжатый пакет меньше порога — нарушение протокола.
	fr = NewFrameReader(bytes.NewReader(zipBomb(10, 9)), 1<<16)
	fr.SetCompression(256)
	if _, _, err := fr.Next(); !errors.Is(err, ErrBadCompressed) {
		t.Fatalf("below threshold: %v", err)
	}
}

func TestTextNBT(t *testing.T) {
	b := AppendTextPlain(nil, "hi")
	if !bytes.Equal(b, []byte{8, 0, 2, 'h', 'i'}) {
		t.Fatalf("plain: %x", b)
	}
	// Символ вне BMP — суррогатная пара по 3 байта (modified UTF-8).
	b = AppendTextPlain(nil, "😀")
	if len(b) != 1+2+6 {
		t.Fatalf("emoji: %x", b)
	}
	b = AppendTextColored(nil, "x", "yellow")
	if b[0] != 10 || b[len(b)-1] != 0 || !strings.Contains(string(b), "yellow") {
		t.Fatalf("colored: %x", b)
	}
}

func TestEncodeAllocs(t *testing.T) {
	pw := NewPacketWriter(256)
	dst := make([]byte, 0, 4096)
	allocs := testing.AllocsPerRun(1000, func() {
		b := pw.Begin(0x2E)
		b = AppendVarInt(b, 42)
		b = AppendShort(b, 100)
		b = AppendShort(b, 0)
		b = AppendShort(b, -100)
		b = AppendBool(b, true)
		dst = pw.Frame(dst[:0], b)
	})
	if allocs != 0 {
		t.Fatalf("encoding a movement packet allocates %.1f times", allocs)
	}
}
