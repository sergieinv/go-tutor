package task

import (
	"encoding/binary"
	"math"
)

const IDEntityPositions = 0x20

type Entity struct {
	ID     int32
	X, Z   float64
	VX, VZ float64
}

// Viewer — игрок: видит сущности в радиусе Radius. Out — его кадры
// за последний тик.
type Viewer struct {
	X, Z   float64
	Radius float64
	Out    []byte
}

type World struct {
	Entities    []Entity
	Viewers     []*Viewer
	MaxPerFrame int // больше сущностей в одном кадре не класть; <= 0 — без лимита

	body []byte // черновик тела кадра: живёт между тиками
}

func appendVarInt(b []byte, v int32) []byte {
	u := uint32(v)
	for u >= 0x80 {
		b = append(b, byte(u)|0x80)
		u >>= 7
	}
	return append(b, byte(u))
}

func (w *World) Tick() {
	for i := range w.Entities {
		e := &w.Entities[i]
		e.X += e.VX
		e.Z += e.VZ
	}
	for _, v := range w.Viewers {
		v.Out = v.Out[:0]
		r2 := v.Radius * v.Radius
		visible := func(e *Entity) bool {
			dx, dz := e.X-v.X, e.Z-v.Z
			return dx*dx+dz*dz <= r2
		}
		total := 0
		for i := range w.Entities {
			if visible(&w.Entities[i]) {
				total++
			}
		}
		per := total
		if w.MaxPerFrame > 0 && w.MaxPerFrame < total {
			per = w.MaxPerFrame
		}
		next := 0 // с какой сущности продолжать
		for written := 0; written == 0 || written < total; {
			n := min(per, total-written)
			b := appendVarInt(w.body[:0], IDEntityPositions)
			b = appendVarInt(b, int32(n))
			for k := 0; k < n; next++ {
				e := &w.Entities[next]
				if !visible(e) {
					continue
				}
				b = appendVarInt(b, e.ID)
				b = binary.BigEndian.AppendUint64(b, math.Float64bits(e.X))
				b = binary.BigEndian.AppendUint64(b, math.Float64bits(e.Z))
				k++
			}
			w.body = b
			v.Out = appendVarInt(v.Out, int32(len(b)))
			v.Out = append(v.Out, b...)
			written += n
			if total == 0 {
				break
			}
		}
	}
}
