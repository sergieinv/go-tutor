// Программа для живого визуализатора escape analysis: песочница
// собирает её с -gcflags=-m, и каждое сообщение компилятора о main.go
// становится шагом «стек» или «куча». Сравните, что уходит в кучу
// в наивном кодировании пакета и в кодировании в переданный буфер.
// Попробуйте: верните из encodeFast новый срез вместо dst, передайте
// x в fmt.Sprint или сделайте буфер в main больше 64 КБ.
package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
)

type Entity struct {
	ID   int32
	X, Z float64
}

func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

// Наивно: буфер уходит в кучу, потому что передаётся в binary.Write
// как io.Writer — куда интерфейс денется внутри, компилятор не знает
// (подробно — -gcflags=-m=2). А binary.Write выделяет срез под байты
// числа на каждый вызов внутри encoding/binary — этого -m для main.go
// не покажет, зато покажет AllocsPerRun.
//
//go:noinline
func encodeNaive(e Entity) []byte {
	body := new(bytes.Buffer)
	body.Write(appendVarInt(nil, e.ID))
	binary.Write(body, binary.BigEndian, e.X)
	binary.Write(body, binary.BigEndian, e.Z)
	return body.Bytes()
}

// Без аллокаций: пишем в буфер вызывающего и возвращаем его же.
// Компилятор видит «leaking param: dst to result» — dst живёт там,
// где решит вызывающий.
//
//go:noinline
func encodeFast(dst []byte, e *Entity) []byte {
	dst = appendVarInt(dst, e.ID)
	dst = binary.BigEndian.AppendUint64(dst, math.Float64bits(e.X))
	return binary.BigEndian.AppendUint64(dst, math.Float64bits(e.Z))
}

// Указатель на новый объект возвращается — объект в куче.
//
//go:noinline
func newEntity(id int32) *Entity {
	return &Entity{ID: id, X: 1.5, Z: -2}
}

func main() {
	e := newEntity(42)
	naive := encodeNaive(*e)

	// Буфер тика: создаётся один раз и обнуляется через [:0].
	// Он не покидает main, поэтому может жить на стеке.
	buf := make([]byte, 0, 64)
	total := 0
	for range 3 {
		buf = encodeFast(buf[:0], e)
		total += len(buf)
	}
	println(total, len(naive)) // println не упаковывает аргументы в any

	fmt.Println("наивный:", len(naive)) // аргументы fmt уходят в кучу
}
