package task

import (
	"errors"
	"math/rand/v2"
	"testing"
)

// safeParse ловит панику, чтобы тест упал с понятным сообщением,
// а не обрушил весь пакет.
func safeParse(b []byte) (f Frame, n int, err error, panicked any) {
	defer func() { panicked = recover() }()
	f, n, err = Parse(b)
	return
}

func safeWalk(b []byte, fn func(Frame)) (n int, err error, panicked any) {
	defer func() { panicked = recover() }()
	n, err = Walk(b, fn)
	return
}

func TestParseEveryPrefixIsShort(t *testing.T) {
	b := frame(1, 7, "0123456789")
	for i := range len(b) {
		_, n, err, p := safeParse(b[:i])
		if p != nil {
			t.Fatalf("Parse(%d байт) паникует: %v", i, p)
		}
		if !errors.Is(err, ErrShort) || n != 0 {
			t.Fatalf("Parse(%d байт) = n %d, err %v, want 0, ErrShort", i, n, err)
		}
	}
}

func TestParseHugeLengthShortBuffer(t *testing.T) {
	b := []byte{'G', 'T', 1, 0, 0xff, 0xff, 'a', 'b'}
	_, n, err, p := safeParse(b)
	if p != nil {
		t.Fatalf("паника: %v", p)
	}
	if !errors.Is(err, ErrShort) || n != 0 {
		t.Fatalf("Parse = %d, %v, want 0, ErrShort", n, err)
	}
	// А полный кадр максимальной длины разбирается.
	full := append(b[:6:6], make([]byte, 65535)...)
	f, n, err, p := safeParse(full)
	if p != nil || err != nil || n != 6+65535 || len(f.Payload) != 65535 {
		t.Fatalf("кадр 64 КБ: n %d, len %d, err %v, panic %v", n, len(f.Payload), err, p)
	}
}

func TestParseErrorOrder(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		// Сначала длина заголовка, потом magic, потом версия, потом данные.
		{"короткий с чужим magic", []byte("XX\x09"), ErrShort},
		{"magic до версии", []byte("GX\x09\x00\x00\x00"), ErrMagic},
		{"второй байт magic", []byte("Gt\x01\x00\x00\x00"), ErrMagic},
		{"версия до длины данных", []byte("GT\x00\x00\x00\x09"), ErrVersion},
		{"версия 0", frame(0, 0, ""), ErrVersion},
	}
	for _, c := range cases {
		_, _, err, p := safeParse(c.in)
		if p != nil {
			t.Fatalf("%s: паника %v", c.name, p)
		}
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
	}
}

func TestPayloadAliasesInputWithCapEqualLen(t *testing.T) {
	var b []byte
	b = append(b, frame(1, 0, "abc")...)
	b = append(b, frame(1, 0, "NEXT")...)
	f, n, err := Parse(b)
	if err != nil || n != 9 {
		t.Fatalf("Parse = %d, %v", n, err)
	}
	if &f.Payload[0] != &b[6] {
		t.Fatal("Payload должен указывать в исходный буфер, без копирования")
	}
	if cap(f.Payload) != len(f.Payload) {
		t.Fatalf("cap(Payload) = %d, want %d — append к Payload затрёт следующий кадр", cap(f.Payload), len(f.Payload))
	}
	_ = append(f.Payload, 'X', 'X')
	if string(b[9:11]) != "GT" {
		t.Fatal("append к Payload испортил следующий кадр")
	}
}

func TestEmptyPayload(t *testing.T) {
	b := append(frame(1, 9, ""), frame(1, 1, "z")...)
	f, n, err := Parse(b)
	if err != nil || n != 6 || f.Flags != 9 || len(f.Payload) != 0 || cap(f.Payload) != 0 {
		t.Fatalf("Parse = (%+v, %d, %v), want пустой Payload с cap 0, n 6", f, n, err)
	}
}

func TestWalkStopsAtError(t *testing.T) {
	var b []byte
	b = append(b, frame(1, 1, "one")...)
	b = append(b, frame(1, 2, "two")...)
	good := len(b)

	cases := []struct {
		name string
		tail []byte
		want error
	}{
		{"обрезанный хвост", frame(1, 3, "three")[:7], ErrShort},
		{"мусор в хвосте", []byte("garbage!"), ErrMagic},
		{"чужая версия", frame(5, 3, "x"), ErrVersion},
	}
	for _, c := range cases {
		in := append(b[:good:good], c.tail...)
		var flags []byte
		n, err, p := safeWalk(in, func(f Frame) { flags = append(flags, f.Flags) })
		if p != nil {
			t.Fatalf("%s: паника %v", c.name, p)
		}
		if n != good || !errors.Is(err, c.want) {
			t.Errorf("%s: Walk = %d, %v, want %d, %v", c.name, n, err, good, c.want)
		}
		if string(flags) != "\x01\x02" {
			t.Errorf("%s: fn вызван для кадров %v, want [1 2]", c.name, flags)
		}
	}
}

func TestWalkEmptyAndNilFn(t *testing.T) {
	if n, err, p := safeWalk(nil, func(Frame) { t.Error("fn вызван на пустом входе") }); p != nil || n != 0 || err != nil {
		t.Fatalf("Walk(nil) = %d, %v, panic %v", n, err, p)
	}
	b := append(frame(1, 0, "a"), frame(1, 0, "bc")...)
	if n, err, p := safeWalk(b, nil); p != nil || n != len(b) || err != nil {
		t.Fatalf("Walk(b, nil) = %d, %v, panic %v", n, err, p)
	}
}

func TestWalkNoAllocs(t *testing.T) {
	var b []byte
	for i := range 20 {
		b = append(b, frame(1, byte(i), "payload-payload")...)
	}
	var total int
	count := func(f Frame) { total += len(f.Payload) }
	allocs := testing.AllocsPerRun(500, func() {
		if _, err := Walk(b, count); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Walk выделяет память: %.0f аллокаций, want 0", allocs)
	}
}

// Псевдослучайные входы с фиксированным seed: никаких паник,
// а на успехе — согласованный результат.
func TestParseRandomInputs(t *testing.T) {
	r := rand.New(rand.NewPCG(1, 2))
	for i := range 20000 {
		size := r.IntN(40)
		b := make([]byte, size)
		for j := range b {
			b[j] = byte(r.UintN(256))
		}
		// Половине входов даём правильное начало, чтобы дойти до длины.
		if size >= 3 && i%2 == 0 {
			b[0], b[1], b[2] = 'G', 'T', 1
			if size >= 6 {
				b[4] = 0
				b[5] = byte(r.IntN(40))
			}
		}
		f, n, err, p := safeParse(b)
		if p != nil {
			t.Fatalf("Parse(% x) паникует: %v", b, p)
		}
		if err == nil {
			want := 6 + int(b[4])<<8 + int(b[5])
			if n != want || len(f.Payload) != want-6 {
				t.Fatalf("Parse(% x) = n %d, len %d, want n %d", b, n, len(f.Payload), want)
			}
		}
		wn, werr, wp := safeWalk(b, nil)
		if wp != nil {
			t.Fatalf("Walk(% x) паникует: %v", b, wp)
		}
		if wn > len(b) || (werr == nil && wn != len(b)) {
			t.Fatalf("Walk(% x) = %d, %v", b, wn, werr)
		}
	}
}
