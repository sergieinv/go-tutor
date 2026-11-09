package task

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"math/rand/v2"
	"runtime"
	"strings"
	"testing"
	"testing/iotest"
)

func TestHiddenVarIntBoundaries(t *testing.T) {
	for _, v := range []int32{0, 1, 127, 128, 16383, 16384, 2097151, 2097152, 268435455, 268435456, math.MaxInt32, math.MinInt32, -1, -128} {
		b := AppendVarInt(nil, v)
		if len(b) > MaxVarIntLen {
			t.Fatalf("AppendVarInt(%d): %d байт", v, len(b))
		}
		r := bytes.NewReader(b)
		got, err := ReadVarInt(r)
		if err != nil || got != v || r.Len() != 0 {
			t.Fatalf("round-trip %d → % x → %d, %v (осталось %d байт)", v, b, got, err, r.Len())
		}
	}
	if n := len(AppendVarInt(nil, -1)); n != 5 {
		t.Fatalf("отрицательное число должно занимать 5 байт, а не %d", n)
	}
}

func TestHiddenVarIntBadForms(t *testing.T) {
	tests := []struct {
		in   []byte
		want error
	}{
		{[]byte{0x80, 0x00}, ErrVarIntNonCanonical},
		{[]byte{0xff, 0x00}, ErrVarIntNonCanonical},
		{[]byte{0x80, 0x80, 0x80, 0x80, 0x00}, ErrVarIntNonCanonical},
		{[]byte{0x80, 0x80, 0x80, 0x80, 0x10}, ErrVarIntOverflow},
		{[]byte{0x80, 0x80, 0x80, 0x80, 0x7f}, ErrVarIntOverflow},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01}, ErrVarIntTooLong},
		{[]byte{0x80, 0x80}, io.ErrUnexpectedEOF},
		{[]byte{0xff, 0xff, 0xff, 0xff}, io.ErrUnexpectedEOF},
	}
	for _, tt := range tests {
		r := bytes.NewReader(tt.in)
		if _, err := ReadVarInt(r); !errors.Is(err, tt.want) {
			t.Errorf("ReadVarInt(% x): err = %v, want %v", tt.in, err, tt.want)
		}
	}
	// Слишком длинное число: читается не больше 5 байт.
	r := bytes.NewReader([]byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01})
	ReadVarInt(r)
	if r.Len() < 3 {
		t.Errorf("ReadVarInt прочитал %d байт, а должен остановиться на пятом", 8-r.Len())
	}
	// Ошибка чтения, отличная от EOF, возвращается как есть.
	boom := errors.New("boom")
	if _, err := ReadVarInt(bufio.NewReader(iotest.ErrReader(boom))); !errors.Is(err, boom) {
		t.Errorf("ошибка ридера: err = %v, want boom", err)
	}
}

func stream() ([]byte, []Frame) {
	frames := []Frame{
		{0x00, []byte{0xff, 0x05, 0x00, 0x63, 0xdd, 0x02}},
		{0x01, append([]byte{0x0c}, "привет!"[:12]...)},
		{0x02, make([]byte, 16)},
		{128, []byte("id из двух байт")},
		{-5, nil},
		{0x01, bytes.Repeat([]byte{'x'}, 300)},
	}
	var wire []byte
	for _, f := range frames {
		wire = AppendFrame(wire, f.ID, f.Data)
	}
	return wire, frames
}

func readAll(t *testing.T, r Reader, max int) []Frame {
	t.Helper()
	var got []Frame
	for {
		f, err := ReadFrame(r, max)
		if err == io.EOF {
			return got
		}
		if err != nil {
			t.Fatalf("кадр %d: %v", len(got), err)
		}
		got = append(got, f)
		if len(got) > 100 {
			t.Fatal("слишком много кадров")
		}
	}
}

func sameFrames(t *testing.T, got, want []Frame) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("кадров %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].ID != want[i].ID || !bytes.Equal(got[i].Data, want[i].Data) {
			t.Fatalf("кадр %d: id %d, % x; want id %d, % x", i, got[i].ID, got[i].Data, want[i].ID, want[i].Data)
		}
	}
}

// Каждый Read отдаёт по одному байту — как медленный TCP.
func TestHiddenOneByteReads(t *testing.T) {
	wire, want := stream()
	sameFrames(t, readAll(t, bufio.NewReaderSize(iotest.OneByteReader(bytes.NewReader(wire)), 16), 1024), want)
}

// Последний Read возвращает данные вместе с io.EOF.
func TestHiddenDataErrReader(t *testing.T) {
	wire, want := stream()
	sameFrames(t, readAll(t, bufio.NewReader(iotest.DataErrReader(bytes.NewReader(wire))), 1024), want)
}

// Кадры, разрезанные на куски случайной длины.
func TestHiddenRandomChunks(t *testing.T) {
	wire, want := stream()
	rng := rand.New(rand.NewPCG(1, 2))
	for range 50 {
		pr, pw := io.Pipe()
		go func() {
			w := wire
			for len(w) > 0 {
				k := min(1+rng.IntN(7), len(w))
				pw.Write(w[:k])
				w = w[k:]
			}
			pw.Close()
		}()
		sameFrames(t, readAll(t, bufio.NewReader(pr), 1024), want)
	}
}

// ReadFrame читает из r ровно байты кадра — следующий кадр не тронут.
func TestHiddenNoOverRead(t *testing.T) {
	wire := AppendFrame(nil, 0x01, []byte("abc"))
	tail := []byte{0xde, 0xad}
	r := bytes.NewReader(append(wire, tail...))
	if _, err := ReadFrame(r, 64); err != nil {
		t.Fatal(err)
	}
	if r.Len() != len(tail) {
		t.Fatalf("после кадра осталось %d байт, want %d", r.Len(), len(tail))
	}
}

func TestHiddenFrameErrors(t *testing.T) {
	tests := []struct {
		name string
		in   []byte
		max  int
		want error
	}{
		{"пусто", nil, 64, io.EOF},
		{"обрыв в длине", []byte{0x80}, 64, io.ErrUnexpectedEOF},
		{"обрыв в теле", []byte{0x0a, 0x01, 0x02, 0x03}, 64, io.ErrUnexpectedEOF},
		{"только длина", []byte{0x03}, 64, io.ErrUnexpectedEOF},
		{"длина 0", []byte{0x00, 0x01}, 64, ErrBadLength},
		{"длина -1", []byte{0xff, 0xff, 0xff, 0xff, 0x0f, 0x01}, 64, ErrBadLength},
		{"длина больше лимита", []byte{0x41}, 64, ErrFrameTooLarge},
		{"лимит 0", []byte{0x01, 0x01}, 0, ErrFrameTooLarge},
		{"длина неканоническая", []byte{0x82, 0x00, 0x01, 0x02}, 64, ErrVarIntNonCanonical},
		{"длина 6 байт", []byte{0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 64, ErrVarIntTooLong},
		{"id обрывается внутри кадра", []byte{0x01, 0x80}, 64, ErrBadLength},
		{"id 6 байт", []byte{0x06, 0x80, 0x80, 0x80, 0x80, 0x80, 0x01}, 64, ErrVarIntTooLong},
		{"id неканонический", []byte{0x02, 0x80, 0x00}, 64, ErrVarIntNonCanonical},
		{"id с переполнением", []byte{0x05, 0xff, 0xff, 0xff, 0xff, 0x7f}, 64, ErrVarIntOverflow},
	}
	for _, tt := range tests {
		if _, err := ReadFrame(bytes.NewReader(tt.in), tt.max); !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
	}
}

// Объявленная длина больше лимита — ошибка без выделения памяти под неё.
func TestHiddenHugeLengthNoAllocation(t *testing.T) {
	evil := AppendVarInt(nil, 64<<20) // 64 МиБ при лимите 1 МиБ
	evil = append(evil, 0x01, 0x02)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for range 3 {
		if _, err := ReadFrame(bytes.NewReader(evil), 1<<20); !errors.Is(err, ErrFrameTooLarge) {
			t.Fatalf("err = %v, want ErrFrameTooLarge", err)
		}
	}
	runtime.ReadMemStats(&after)
	if d := after.TotalAlloc - before.TotalAlloc; d > 1<<20 {
		t.Fatalf("на три отказа выделено %d байт — память выделяется до проверки длины", d)
	}
}

func TestHiddenAppendFrame(t *testing.T) {
	big := bytes.Repeat([]byte{7}, 200)
	got := AppendFrame([]byte{0xee}, 300, big)
	want := append([]byte{0xee}, AppendVarInt(nil, int32(2+len(big)))...)
	want = append(want, 0xac, 0x02)
	want = append(want, big...)
	if !bytes.Equal(got, want) {
		t.Fatalf("AppendFrame(ee, 300, 200 байт): длина префикса или id неверны")
	}
	f, err := ReadFrame(bytes.NewReader(got[1:]), 1024)
	if err != nil || f.ID != 300 || !bytes.Equal(f.Data, big) {
		t.Fatalf("round-trip: %d, %v", f.ID, err)
	}
}

func str(s string) []byte { return append(AppendVarInt(nil, int32(len(s))), s...) }

func handshake(addr []byte) []byte {
	b := AppendVarInt(nil, 767)
	b = append(b, addr...)
	b = binary.BigEndian.AppendUint16(b, 25565)
	return AppendVarInt(b, 2)
}

func TestHiddenDecodeLimits(t *testing.T) {
	tests := []struct {
		name string
		f    Frame
		want error
	}{
		{"адрес 256 байт", Frame{0x00, handshake(str(strings.Repeat("a", 256)))}, ErrStringTooLong},
		{"чат 257 байт", Frame{0x01, str(strings.Repeat("a", 257))}, ErrStringTooLong},
		{"чат 129 «ж» = 258 байт", Frame{0x01, str(strings.Repeat("ж", 129))}, ErrStringTooLong},
		{"огромная длина строки", Frame{0x01, append(AppendVarInt(nil, math.MaxInt32), 'a')}, ErrStringTooLong},
		{"отрицательная длина строки", Frame{0x01, append(AppendVarInt(nil, -1), 'a')}, ErrBadLength},
		{"строка длиннее пакета", Frame{0x01, append(AppendVarInt(nil, 10), 'a')}, ErrShortPacket},
		{"битый UTF-8", Frame{0x01, str("\xff\xfe")}, ErrBadUTF8},
		{"обрыв UTF-8", Frame{0x01, str("ж"[:1])}, ErrBadUTF8},
		{"пустой чат", Frame{0x01, nil}, ErrShortPacket},
		{"нет порта", Frame{0x00, append(AppendVarInt(nil, 767), str("a")...)}, ErrShortPacket},
		{"полпорта", Frame{0x00, append(append(AppendVarInt(nil, 767), str("a")...), 0x63)}, ErrShortPacket},
		{"нет next state", Frame{0x00, append(append(AppendVarInt(nil, 767), str("a")...), 0x63, 0xdd)}, ErrShortPacket},
		{"обрыв VarInt протокола", Frame{0x00, []byte{0xff}}, ErrShortPacket},
		{"неканонический VarInt в полях", Frame{0x00, []byte{0x80, 0x00}}, ErrVarIntNonCanonical},
		{"move 15 байт", Frame{0x02, make([]byte, 15)}, ErrShortPacket},
		{"move 17 байт", Frame{0x02, make([]byte, 17)}, ErrTrailingData},
		{"лишнее после handshake", Frame{0x00, append(handshake(str("a")), 0)}, ErrTrailingData},
		{"id 3", Frame{0x03, nil}, ErrUnknownPacket},
		{"id -1", Frame{-1, []byte{1}}, ErrUnknownPacket},
	}
	for _, tt := range tests {
		p, err := DecodePacket(tt.f)
		if !errors.Is(err, tt.want) {
			t.Errorf("%s: err = %v, want %v", tt.name, err, tt.want)
		}
		if err != nil && p != nil {
			t.Errorf("%s: при ошибке пакет должен быть nil, а не %#v", tt.name, p)
		}
	}
}

func TestHiddenDecodeEdges(t *testing.T) {
	cases := []struct {
		f    Frame
		want any
	}{
		{Frame{0x00, handshake(str(strings.Repeat("a", 255)))}, Handshake{767, strings.Repeat("a", 255), 25565, 2}},
		{Frame{0x00, handshake(str(""))}, Handshake{767, "", 25565, 2}},
		{Frame{0x01, str(strings.Repeat("a", 256))}, Chat{strings.Repeat("a", 256)}},
		{Frame{0x01, str(strings.Repeat("ж", 128))}, Chat{strings.Repeat("ж", 128)}},
		{Frame{0x01, str("")}, Chat{""}},
		{Frame{0x02, binary.BigEndian.AppendUint64(binary.BigEndian.AppendUint64(nil, math.Float64bits(-0.25)), math.Float64bits(1e300))}, Move{-0.25, 1e300}},
	}
	for i, c := range cases {
		p, err := DecodePacket(c.f)
		if err != nil || p != c.want {
			t.Errorf("случай %d: %#v, %v; want %#v", i, p, err, c.want)
		}
	}
}

var known = []error{
	io.EOF, io.ErrUnexpectedEOF, ErrVarIntTooLong, ErrVarIntOverflow, ErrVarIntNonCanonical,
	ErrBadLength, ErrFrameTooLarge, ErrUnknownPacket, ErrShortPacket, ErrStringTooLong, ErrBadUTF8, ErrTrailingData,
}

func isKnown(err error) bool {
	for _, k := range known {
		if errors.Is(err, k) {
			return true
		}
	}
	return false
}

// То, что нашёл бы фаззер: случайные байты с фиксированным зерном.
// Ничего не паникует, ошибки — только из списка, принятый кадр
// записывается обратно ровно в прочитанные байты.
func TestHiddenRandomBytes(t *testing.T) {
	rng := rand.New(rand.NewPCG(42, 7))
	for i := range 20000 {
		data := make([]byte, rng.IntN(24))
		for j := range data {
			switch rng.IntN(4) {
			case 0:
				data[j] = 0x80 | byte(rng.IntN(128))
			case 1:
				data[j] = byte(rng.IntN(4))
			default:
				data[j] = byte(rng.Uint32())
			}
		}
		r := bytes.NewReader(data)
		f, err := ReadFrame(r, 16)
		if err != nil {
			if !isKnown(err) {
				t.Fatalf("вход %d (% x): неожиданная ошибка %v", i, data, err)
			}
			continue
		}
		used := len(data) - r.Len()
		if again := AppendFrame(nil, f.ID, f.Data); !bytes.Equal(again, data[:used]) {
			t.Fatalf("вход % x: прочитано % x, записано % x", data, data[:used], again)
		}
		for _, id := range []int32{0, 1, 2} {
			if _, err := DecodePacket(Frame{id, f.Data}); err != nil && !isKnown(err) {
				t.Fatalf("DecodePacket(%d, % x): неожиданная ошибка %v", id, f.Data, err)
			}
		}
	}
}

func FuzzHiddenDecode(f *testing.F) {
	f.Add(int32(0), handshake(str("localhost")))
	f.Add(int32(1), str("hi"))
	f.Add(int32(2), make([]byte, 16))
	f.Add(int32(1), []byte{0xff, 0xff, 0xff, 0xff, 0x07})
	f.Fuzz(func(t *testing.T, id int32, data []byte) {
		if _, err := DecodePacket(Frame{id, data}); err != nil && !isKnown(err) {
			t.Fatalf("неожиданная ошибка %v", err)
		}
	})
}
