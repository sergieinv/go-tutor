// Тесты — во внешнем пакете: они видят только экспортированное.
// Модуль песочницы называется `lesson`.
package frame_test

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"

	frame "lesson"
)

// Таблица кодирования. 25565 — порт Minecraft по умолчанию;
// 2097151 — наибольшая длина пакета Minecraft, ровно 3 байта.
func TestVarIntEncoding(t *testing.T) {
	tests := []struct {
		v    int32
		want []byte
	}{
		{0, []byte{0x00}},
		{127, []byte{0x7f}},
		{128, []byte{0x80, 0x01}},
		{25565, []byte{0xdd, 0xc7, 0x01}},
		{2097151, []byte{0xff, 0xff, 0x7f}},
		{2147483647, []byte{0xff, 0xff, 0xff, 0xff, 0x07}},
		{-1, []byte{0xff, 0xff, 0xff, 0xff, 0x0f}},
	}
	for _, tt := range tests {
		if got := frame.AppendVarInt(nil, tt.v); !bytes.Equal(got, tt.want) {
			t.Errorf("AppendVarInt(%d) = % x, want % x", tt.v, got, tt.want)
		}
	}
}

// Битый ввод — ошибки, а не мусорные числа.
func TestVarIntRejects(t *testing.T) {
	tests := []struct {
		in   []byte
		want error
	}{
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x80, 0x01}, frame.ErrVarIntTooLong},
		{[]byte{0xff, 0xff, 0xff, 0xff, 0x1f}, frame.ErrVarIntOverflow},
		{[]byte{0x80, 0x00}, frame.ErrVarIntNonCanonical},
		{[]byte{0x80}, io.ErrUnexpectedEOF},
		{nil, io.EOF},
	}
	for _, tt := range tests {
		_, err := frame.ReadVarInt(bytes.NewReader(tt.in))
		if !errors.Is(err, tt.want) {
			t.Errorf("ReadVarInt(% x): err = %v, want %v", tt.in, err, tt.want)
		}
	}
}

// Почему не `binary.Uvarint`: это формат uint64 до 10 байт.
// Он примет и шесть байт, и число больше 32 бит, и ноль в двух
// байтах. Для протокола с int32 нужен свой декодер с лимитом.
func TestUvarintIsNotEnough(t *testing.T) {
	v, n := binary.Uvarint([]byte{0xff, 0xff, 0xff, 0xff, 0xff, 0x01})
	if n != 6 || v <= 1<<32 {
		t.Fatalf("Uvarint = %d, %d", v, n)
	}
	if v, n := binary.Uvarint([]byte{0x80, 0x00}); v != 0 || n != 2 {
		t.Fatalf("Uvarint(80 00) = %d, %d", v, n)
	}
}

// TCP — поток: три кадра приходят кусками по 1, 2, 3… байта,
// куски не совпадают с границами кадров. `net.Pipe` — соединение
// в памяти, у которого каждый `Write` — ровно один кусок для
// `Read`; на loopback-TCP границы были бы ещё случайнее.
func TestFramesOverStream(t *testing.T) {
	var wire []byte
	for _, s := range []string{"привет", "", "мир"} {
		body := append(frame.AppendVarInt(nil, 0x01), s...)
		wire = frame.AppendFrame(wire, body)
	}
	client, server := net.Pipe()
	go func() {
		defer client.Close()
		for size := 1; len(wire) > 0; size++ {
			k := min(size, len(wire))
			client.Write(wire[:k])
			wire = wire[k:]
		}
	}()
	br := bufio.NewReader(server)
	var got []string
	for {
		body, err := frame.ReadFrame(br, frame.MaxFrame)
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		got = append(got, string(body[1:])) // body[0] — id 0x01
	}
	if len(got) != 3 || got[0] != "привет" || got[1] != "" || got[2] != "мир" {
		t.Fatalf("got %q", got)
	}
}

// Клиент объявил длину 2147483647. Ответ — ошибка до выделения
// памяти: `testing.AllocsPerRun` не видит ни одной аллокации.
func TestHugeLengthNoAlloc(t *testing.T) {
	evil := frame.AppendVarInt(nil, 1<<31-1)
	rd := bytes.NewReader(evil)
	br := bufio.NewReader(rd)
	allocs := testing.AllocsPerRun(100, func() {
		rd.Reset(evil)
		br.Reset(rd)
		if _, err := frame.ReadFrame(br, frame.MaxFrame); err != frame.ErrFrameTooLarge {
			t.Fatalf("err = %v", err)
		}
	})
	if allocs != 0 {
		t.Fatalf("аллокаций на вызов: %v", allocs)
	}
}

// Свойство для фаззера: любое int32 переживает кодирование и
// декодирование и занимает не больше 5 байт.
func FuzzVarInt(f *testing.F) {
	f.Add(int32(0))
	f.Add(int32(-1))
	f.Add(int32(25565))
	f.Fuzz(func(t *testing.T, v int32) {
		b := frame.AppendVarInt(nil, v)
		if len(b) > frame.MaxVarIntLen {
			t.Fatalf("%d: %d байт", v, len(b))
		}
		got, err := frame.ReadVarInt(bytes.NewReader(b))
		if err != nil || got != v {
			t.Fatalf("%d → % x → %d, %v", v, b, got, err)
		}
	})
}

// Главный fuzz-тест: на любых байтах `ReadFrame` не паникует,
// а если кадр принят, то его повторная запись даёт ровно те
// байты, что были прочитаны. Мусор не превращается в «почти
// правильный» пакет.
func FuzzReadFrame(f *testing.F) {
	f.Add(frame.AppendFrame(nil, []byte{0x01, 'h', 'i'}))
	f.Add([]byte{0xff, 0xff, 0xff, 0xff, 0x07})
	f.Add([]byte{0x80, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, data []byte) {
		rd := bytes.NewReader(data)
		br := bufio.NewReader(rd)
		body, err := frame.ReadFrame(br, 64)
		if err != nil {
			return
		}
		used := len(data) - rd.Len() - br.Buffered()
		if again := frame.AppendFrame(nil, body); !bytes.Equal(again, data[:used]) {
			t.Fatalf("прочитано % x, записано % x", data[:used], again)
		}
	})
}
