package task

import (
	"errors"
	"testing"
)

// frame собирает кадр: "GT", версия, флаги, длина (big-endian), данные.
func frame(version, flags byte, payload string) []byte {
	n := len(payload)
	b := []byte{'G', 'T', version, flags, byte(n >> 8), byte(n)}
	return append(b, payload...)
}

func TestParseOne(t *testing.T) {
	b := frame(1, 0x05, "hello")
	f, n, err := Parse(b)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if n != 11 || f.Flags != 0x05 || string(f.Payload) != "hello" {
		t.Fatalf("Parse = (%+v, %d), want Flags 5, Payload hello, n 11", f, n)
	}
}

func TestParseErrors(t *testing.T) {
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"пустой", nil, ErrShort},
		{"неполный заголовок", []byte("GT\x01"), ErrShort},
		{"чужой magic", []byte("XT\x01\x00\x00\x00"), ErrMagic},
		{"версия 2", frame(2, 0, "x"), ErrVersion},
		{"данные обрезаны", frame(1, 0, "hello")[:8], ErrShort},
	}
	for _, c := range cases {
		f, n, err := Parse(c.in)
		if !errors.Is(err, c.want) {
			t.Errorf("%s: err = %v, want %v", c.name, err, c.want)
		}
		if n != 0 || f.Flags != 0 || f.Payload != nil {
			t.Errorf("%s: при ошибке нужен нулевой Frame и n = 0, got (%+v, %d)", c.name, f, n)
		}
	}
}

func TestWalk(t *testing.T) {
	var b []byte
	b = append(b, frame(1, 1, "ab")...)
	b = append(b, frame(1, 2, "")...)
	b = append(b, frame(1, 3, "xyz")...)
	var got []string
	n, err := Walk(b, func(f Frame) { got = append(got, string(rune('0'+f.Flags))+":"+string(f.Payload)) })
	if err != nil || n != len(b) {
		t.Fatalf("Walk = %d, %v, want %d, nil", n, err, len(b))
	}
	want := []string{"1:ab", "2:", "3:xyz"}
	if len(got) != len(want) {
		t.Fatalf("кадры = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("кадры = %q, want %q", got, want)
		}
	}
}

func TestParseNoAllocs(t *testing.T) {
	b := frame(1, 0, "payload")
	allocs := testing.AllocsPerRun(1000, func() {
		if _, _, err := Parse(b); err != nil {
			panic(err)
		}
	})
	if allocs != 0 {
		t.Fatalf("Parse выделяет память: %.0f аллокаций, want 0", allocs)
	}
}
