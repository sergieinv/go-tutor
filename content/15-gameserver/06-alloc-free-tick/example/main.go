// Тик рассылки позиций сущностей в двух вариантах. Наивный
// собирает каждый кадр заново: новые срезы, `bytes.Buffer`,
// `binary.Write`. Тик без аллокаций пишет в буферы, которые живут
// между тиками: срезы обнуляются через `[:0]`, числа дописываются
// `binary.BigEndian.Append*` и своей `appendVarInt`. Байты кадров
// одинаковые — тесты это проверяют, а `AllocsPerRun` и бенчмарки
// показывают разницу.
package main

import (
	"bytes"
	"encoding/binary"
	"math"
)

const idEntityPositions = 0x20 // учебный id пакета

type Entity struct {
	ID     int32
	X, Z   float64
	VX, VZ float64
}

// Игрок видит сущности в радиусе R. `Out` — его кадр за тик.
type Client struct {
	X, Z float64
	Out  []byte
}

type World struct {
	Entities []Entity
	Clients  []*Client
	R        float64
	body     []byte // черновик тела кадра, переживает тики
}

// VarInt (урок 01): 7 бит на байт, старший — «есть продолжение».
// int32 кодируется как uint32: отрицательные занимают 5 байт.
func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func (w *World) move() {
	for i := range w.Entities {
		e := &w.Entities[i] // указатель на элемент, а не копия
		e.X += e.VX
		e.Z += e.VZ
	}
}

func (w *World) visible(c *Client, e *Entity) bool {
	dx, dz := e.X-c.X, e.Z-c.Z
	return dx*dx+dz*dz <= w.R*w.R
}

// Наивный тик: так пишут, пока не посмотрят в профиль.
// Каждый вызов — десятки выделений на игрока.
func (w *World) TickNaive() {
	w.move()
	for _, c := range w.Clients {
		var near []Entity // растёт append'ом с нуля каждый тик
		for _, e := range w.Entities {
			if w.visible(c, &e) {
				near = append(near, e)
			}
		}
		var body bytes.Buffer
		body.Write(appendVarInt(nil, int32(len(near))))
		for _, e := range near {
			body.Write(appendVarInt(nil, e.ID))
			binary.Write(&body, binary.BigEndian, e.X) // make([]byte, 8) внутри
			binary.Write(&body, binary.BigEndian, e.Z)
		}
		payload := append(appendVarInt(nil, idEntityPositions), body.Bytes()...)
		c.Out = append(appendVarInt(nil, int32(len(payload))), payload...)
	}
}

// Тик без аллокаций. Длина кадра заранее неизвестна, поэтому тело
// пишется в черновик `w.body`, а потом заголовок и тело — в `c.Out`.
// Оба среза только обнуляются: ёмкость, набранная на прогреве,
// переиспользуется.
func (w *World) TickFast() {
	w.move()
	for _, c := range w.Clients {
		n := int32(0)
		for i := range w.Entities {
			if w.visible(c, &w.Entities[i]) {
				n++
			}
		}
		b := appendVarInt(w.body[:0], idEntityPositions)
		b = appendVarInt(b, n)
		for i := range w.Entities {
			e := &w.Entities[i]
			if !w.visible(c, e) {
				continue
			}
			b = appendVarInt(b, e.ID)
			b = binary.BigEndian.AppendUint64(b, math.Float64bits(e.X))
			b = binary.BigEndian.AppendUint64(b, math.Float64bits(e.Z))
		}
		w.body = b
		c.Out = appendVarInt(c.Out[:0], int32(len(b)))
		c.Out = append(c.Out, b...)
	}
}

// Мир для тестов и бенчмарков: детерминированный, без случайности.
func NewWorld(entities, clients int) *World {
	w := &World{R: 48}
	for i := range entities {
		f := float64(i)
		w.Entities = append(w.Entities, Entity{ID: int32(i*7 - 50), X: f * 3, Z: f * 2, VX: 0.25, VZ: -0.5})
	}
	for i := range clients {
		w.Clients = append(w.Clients, &Client{X: float64(i * 40), Z: float64(i * 25)})
	}
	return w
}

func main() {}
