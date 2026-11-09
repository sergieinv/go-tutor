package task

import (
	"errors"
	"slices"
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
)

func marshal(t *testing.T, m proto.Message) []byte {
	t.Helper()
	b, err := proto.MarshalOptions{Deterministic: true}.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestFieldsOrder(t *testing.T) {
	b := marshal(t, &Order{Id: 42, CustomerEmail: "ann@example.com", Tags: []string{"gift", "app"}})
	got, err := Fields(b)
	if err != nil {
		t.Fatalf("Fields: %v", err)
	}
	want := []Field{
		{1, protowire.VarintType},
		{2, protowire.BytesType},
		{4, protowire.BytesType},
		{4, protowire.BytesType},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("Fields = %v, want %v", got, want)
	}
}

func TestFieldsEmpty(t *testing.T) {
	got, err := Fields(nil)
	if err != nil || len(got) != 0 {
		t.Fatalf("Fields(nil) = %v, %v; want пусто и nil", got, err)
	}
}

func TestVarint(t *testing.T) {
	b := marshal(t, &Order{Id: 42, DiscountPercent: proto.Uint32(15)})
	v, ok, err := Varint(b, 3)
	if err != nil || !ok || v != 15 {
		t.Fatalf("Varint(discount) = %d, %v, %v; want 15, true, nil", v, ok, err)
	}
	v, ok, err = Varint(b, 1)
	if err != nil || !ok || v != 42 {
		t.Fatalf("Varint(id) = %d, %v, %v; want 42, true, nil", v, ok, err)
	}
}

func TestVarintAbsent(t *testing.T) {
	// Скидка не задана, а id = 0 в proto3 на провод не попадает.
	b := marshal(t, &Order{CustomerEmail: "ann@example.com"})
	for _, num := range []protowire.Number{1, 3} {
		v, ok, err := Varint(b, num)
		if err != nil || ok || v != 0 {
			t.Fatalf("Varint(%d) = %d, %v, %v; want 0, false, nil", num, v, ok, err)
		}
	}
}

func TestVarintWrongType(t *testing.T) {
	b := marshal(t, &Order{CustomerEmail: "ann@example.com"})
	_, ok, err := Varint(b, 2)
	if !errors.Is(err, ErrWireType) || ok {
		t.Fatalf("Varint(поле-строка) = ok %v, err %v; want ErrWireType", ok, err)
	}
}

func TestMalformedTruncated(t *testing.T) {
	b := marshal(t, &Order{CustomerEmail: "ann@example.com"})
	b = b[:len(b)-1] // строка обрезана
	if _, err := Fields(b); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Fields(обрезанное) err = %v, want ErrMalformed", err)
	}
	if _, _, err := Varint(b, 1); !errors.Is(err, ErrMalformed) {
		t.Fatalf("Varint(обрезанное) err = %v, want ErrMalformed", err)
	}
}
