package task

import (
	"errors"
	"math"
	"slices"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func TestHiddenLastWinsAfterConcat(t *testing.T) {
	// Склейка двух сообщений — это слияние: скалярное поле берётся
	// из последнего вхождения, отсутствующее — не затирает прежнее.
	first := marshal(t, &Order{Id: 1, DiscountPercent: proto.Uint32(10)})
	second := marshal(t, &Order{Id: 2})
	b := append(slices.Clone(first), second...)
	if v, ok, err := Varint(b, 1); err != nil || !ok || v != 2 {
		t.Fatalf("Varint(id) = %d, %v, %v; want 2, true, nil", v, ok, err)
	}
	if v, ok, err := Varint(b, 3); err != nil || !ok || v != 10 {
		t.Fatalf("Varint(discount) = %d, %v, %v; want 10, true, nil", v, ok, err)
	}
	// Явный 0 у optional-поля кодируется и побеждает.
	zero := marshal(t, &Order{DiscountPercent: proto.Uint32(0)})
	b = append(b, zero...)
	if v, ok, err := Varint(b, 3); err != nil || !ok || v != 0 {
		t.Fatalf("Varint(discount=0) = %d, %v, %v; want 0, true, nil", v, ok, err)
	}
	var m Order
	if err := proto.Unmarshal(b, &m); err != nil || m.GetId() != 2 || m.DiscountPercent == nil || *m.DiscountPercent != 0 {
		t.Fatalf("proto.Unmarshal не согласен с тестом: %v %v", &m, err)
	}
}

func TestHiddenNegativeAndZigZag(t *testing.T) {
	b := marshal(t, &Order{Id: -1, BalanceDelta: -3})
	v, ok, err := Varint(b, 1)
	if err != nil || !ok || v != math.MaxUint64 {
		t.Fatalf("Varint(id=-1) = %d, %v, %v; want MaxUint64 (int64(-1) в varint)", v, ok, err)
	}
	v, ok, err = Varint(b, 6)
	if err != nil || !ok || protowire.DecodeZigZag(v) != -3 {
		t.Fatalf("Varint(sint64=-3) = %d, %v, %v; want zigzag 5", v, ok, err)
	}
	if len(b) != 1+10+1+1 {
		t.Fatalf("размер = %d", len(b))
	}
}

func TestHiddenFixedAndUnknown(t *testing.T) {
	b := marshal(t, &Order{Checksum: 7})
	b = protowire.AppendTag(b, 100, protowire.Fixed32Type)
	b = protowire.AppendFixed32(b, 1)
	b = protowire.AppendTag(b, 1, protowire.VarintType)
	b = protowire.AppendVarint(b, 300)
	got, err := Fields(b)
	want := []Field{{5, protowire.Fixed64Type}, {100, protowire.Fixed32Type}, {1, protowire.VarintType}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("Fields = %v, %v; want %v", got, err, want)
	}
	if _, _, err := Varint(b, 5); !errors.Is(err, ErrWireType) {
		t.Fatalf("Varint(fixed64) err = %v, want ErrWireType", err)
	}
	if v, ok, err := Varint(b, 1); err != nil || !ok || v != 300 {
		t.Fatalf("Varint(1) = %d, %v, %v; want 300", v, ok, err)
	}
	if v, ok, err := Varint(b, 9); err != nil || ok || v != 0 {
		t.Fatalf("Varint(нет поля) = %d, %v, %v", v, ok, err)
	}
}

func TestHiddenEveryPrefix(t *testing.T) {
	b := marshal(t, &Order{
		Id: 1 << 40, CustomerEmail: "ann@example.com", DiscountPercent: proto.Uint32(300),
		Tags: []string{"a", "bb"}, Checksum: 9, BalanceDelta: -100,
	})
	// Границы полей — там, где обрезка оставляет корректное сообщение.
	bounds := map[int]bool{0: true}
	for rest := b; len(rest) > 0; {
		_, _, n := protowire.ConsumeField(rest)
		rest = rest[n:]
		bounds[len(b)-len(rest)] = true
	}
	for i := 0; i <= len(b); i++ {
		_, err := Fields(b[:i])
		_, _, verr := Varint(b[:i], 1)
		if bounds[i] {
			if err != nil || verr != nil {
				t.Fatalf("префикс %d (граница поля): Fields err %v, Varint err %v", i, err, verr)
			}
			continue
		}
		if !errors.Is(err, ErrMalformed) || !errors.Is(verr, ErrMalformed) {
			t.Fatalf("префикс %d: Fields err %v, Varint err %v; want ErrMalformed", i, err, verr)
		}
	}
}

func TestHiddenBadInput(t *testing.T) {
	cases := map[string][]byte{
		"номер поля 0":        {0x00, 0x01},
		"группа":              protowire.AppendTag(nil, 1, protowire.StartGroupType),
		"конец группы":        protowire.AppendTag(nil, 1, protowire.EndGroupType),
		"wire type 6":         {0x0e, 0x01},
		"wire type 7":         {0x0f, 0x01},
		"длина больше данных": {0x12, 0x05, 'a', 'b'},
		"переполнение varint": {0x08, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0x01},
		"обрезанный тег":      {0x80},
		"ошибка после поля":   {0x08, 0x01, 0x12},
	}
	for name, b := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := Fields(b)
			if !errors.Is(err, ErrMalformed) || got != nil {
				t.Fatalf("Fields = %v, %v; want nil, ErrMalformed", got, err)
			}
			v, ok, err := Varint(b, 1)
			if !errors.Is(err, ErrMalformed) || ok || v != 0 {
				t.Fatalf("Varint = %d, %v, %v; want 0, false, ErrMalformed", v, ok, err)
			}
		})
	}
}

func TestHiddenMalformedBeatsWireType(t *testing.T) {
	// Поле 2 — строка (не varint), а за ним мусор: сначала проверяется
	// всё сообщение.
	b := marshal(t, &Order{CustomerEmail: "x"})
	b = append(b, 0x0e)
	if _, _, err := Varint(b, 2); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Varint err = %v, want ErrMalformed", err)
	}
}
