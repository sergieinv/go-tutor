package task

import (
	"bytes"
	"encoding/binary"
	"math"
	"testing"
)

// refVarInt и refFrames — эталонное кодирование, написанное в лоб
// (с выделениями — в тестах можно).
func refVarInt(v int32) []byte {
	var out []byte
	u := uint32(v)
	for u >= 0x80 {
		out = append(out, byte(u)|0x80)
		u >>= 7
	}
	return append(out, byte(u))
}

func refFrame(ents []Entity) []byte {
	payload := refVarInt(IDEntityPositions)
	payload = append(payload, refVarInt(int32(len(ents)))...)
	for _, e := range ents {
		payload = append(payload, refVarInt(e.ID)...)
		var b [8]byte
		binary.BigEndian.PutUint64(b[:], math.Float64bits(e.X))
		payload = append(payload, b[:]...)
		binary.BigEndian.PutUint64(b[:], math.Float64bits(e.Z))
		payload = append(payload, b[:]...)
	}
	return append(refVarInt(int32(len(payload))), payload...)
}

// refOut — кадры игрока для уже сдвинутых сущностей.
func refOut(ents []Entity, v *Viewer, maxPer int) []byte {
	var vis []Entity
	for _, e := range ents {
		dx, dz := e.X-v.X, e.Z-v.Z
		if dx*dx+dz*dz <= v.Radius*v.Radius {
			vis = append(vis, e)
		}
	}
	if maxPer <= 0 || maxPer > len(vis) {
		maxPer = max(len(vis), 1)
	}
	var out []byte
	for i := 0; i == 0 || i < len(vis); i += maxPer {
		out = append(out, refFrame(vis[i:min(i+maxPer, len(vis))])...)
	}
	return out
}

// moved — сущности после одного тика.
func moved(ents []Entity) []Entity {
	out := make([]Entity, len(ents))
	for i, e := range ents {
		e.X += e.VX
		e.Z += e.VZ
		out[i] = e
	}
	return out
}

func TestOneFrame(t *testing.T) {
	w := &World{
		Entities: []Entity{
			{ID: 1, X: 0, Z: 0, VX: 1, VZ: 0},
			{ID: 300, X: 10, Z: 10},
			{ID: -1, X: 2, Z: 2, VZ: 1},
		},
		Viewers: []*Viewer{{X: 0, Z: 0, Radius: 5}},
	}
	w.Tick()
	// видимы 1 (в 1, 0) и -1 (в 2, 3); 300 далеко
	want := refFrame([]Entity{{ID: 1, X: 1, Z: 0}, {ID: -1, X: 2, Z: 3}})
	if got := w.Viewers[0].Out; !bytes.Equal(got, want) {
		t.Fatalf("Out = % x\nwant  % x", got, want)
	}
	if w.Entities[0].X != 1 || w.Entities[2].Z != 3 {
		t.Fatalf("сущности не сдвинуты: %+v", w.Entities)
	}
}

func TestEmptyView(t *testing.T) {
	w := &World{Entities: []Entity{{ID: 7, X: 100}}, Viewers: []*Viewer{{Radius: 1}}}
	w.Tick()
	if got, want := w.Viewers[0].Out, []byte{0x02, 0x20, 0x00}; !bytes.Equal(got, want) {
		t.Fatalf("Out = % x, want % x — один кадр с нулём сущностей", got, want)
	}
}

func TestSplitFrames(t *testing.T) {
	var ents []Entity
	for i := range 5 {
		ents = append(ents, Entity{ID: int32(i), X: float64(i)})
	}
	w := &World{Entities: ents, Viewers: []*Viewer{{Radius: 100}}, MaxPerFrame: 2}
	w.Tick()
	want := refOut(moved(ents), &Viewer{Radius: 100}, 2)
	if got := w.Viewers[0].Out; !bytes.Equal(got, want) {
		t.Fatalf("Out = % x\nwant  % x — кадры по 2, 2 и 1 сущности", got, want)
	}
}

func TestNoAllocsAfterWarmup(t *testing.T) {
	w := &World{}
	for i := range 500 {
		w.Entities = append(w.Entities, Entity{ID: int32(i), X: float64(i % 50), Z: float64(i / 50)})
	}
	for i := range 20 {
		w.Viewers = append(w.Viewers, &Viewer{X: float64(i * 3), Z: 5, Radius: 12})
	}
	w.Tick()
	if n := testing.AllocsPerRun(50, w.Tick); n != 0 {
		t.Fatalf("Tick: %v выделений на тик после прогрева, want 0", n)
	}
	for i, v := range w.Viewers {
		if !bytes.Equal(v.Out, refOut(w.Entities, v, 0)) {
			t.Fatalf("игрок %d: кадры не совпали с эталоном", i)
		}
	}
}
