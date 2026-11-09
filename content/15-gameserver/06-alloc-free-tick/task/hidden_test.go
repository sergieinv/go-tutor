package task

import (
	"bytes"
	"math"
	"math/rand/v2"
	"testing"
)

// Случайные миры с фиксированным зерном: несколько тиков подряд,
// кадры совпадают с эталоном байт в байт.
func TestHiddenRandomWorlds(t *testing.T) {
	rng := rand.New(rand.NewPCG(6, 15))
	for trial := range 40 {
		var ents []Entity
		for range rng.IntN(60) {
			ents = append(ents, Entity{
				ID: rng.Int32() - math.MaxInt32/2, // и отрицательные: VarInt в 5 байт
				X:  rng.Float64()*200 - 100, Z: rng.Float64()*200 - 100,
				VX: rng.Float64()*4 - 2, VZ: rng.Float64()*4 - 2,
			})
		}
		var viewers []*Viewer
		for range 1 + rng.IntN(6) {
			viewers = append(viewers, &Viewer{X: rng.Float64()*200 - 100, Z: rng.Float64()*200 - 100, Radius: rng.Float64() * 80})
		}
		maxPer := rng.IntN(8) - 2 // и <= 0 — без лимита
		w := &World{Entities: append([]Entity(nil), ents...), Viewers: viewers, MaxPerFrame: maxPer}
		for tick := range 4 {
			ents = moved(ents)
			w.Tick()
			for i, v := range viewers {
				if want := refOut(ents, v, maxPer); !bytes.Equal(v.Out, want) {
					t.Fatalf("испытание %d, тик %d, игрок %d (MaxPerFrame %d): кадры не совпали\n got % x\nwant % x", trial, tick+1, i, maxPer, v.Out, want)
				}
			}
		}
	}
}

// Расстояние ровно R — сущность видна; радиус 0 — видна только
// сущность в той же точке.
func TestHiddenBoundary(t *testing.T) {
	w := &World{
		Entities: []Entity{{ID: 1, X: 3, Z: 4}, {ID: 2, X: 3, Z: 4.000001}, {ID: 3, X: -3, Z: -4}},
		Viewers:  []*Viewer{{Radius: 5}, {X: 3, Z: 4, Radius: 0}},
	}
	w.Tick()
	want0 := refFrame([]Entity{{ID: 1, X: 3, Z: 4}, {ID: 3, X: -3, Z: -4}})
	if !bytes.Equal(w.Viewers[0].Out, want0) {
		t.Fatalf("радиус 5: Out = % x, want % x — на расстоянии ровно R видна", w.Viewers[0].Out, want0)
	}
	want1 := refFrame([]Entity{{ID: 1, X: 3, Z: 4}})
	if !bytes.Equal(w.Viewers[1].Out, want1) {
		t.Fatalf("радиус 0: Out = % x, want % x", w.Viewers[1].Out, want1)
	}
}

// Out заменяется каждый тик, а не дописывается.
func TestHiddenOutReplaced(t *testing.T) {
	v := &Viewer{Radius: 10, Out: []byte("мусор от прошлого тика")}
	w := &World{Entities: []Entity{{ID: 5, VX: 1}}, Viewers: []*Viewer{v}}
	for range 3 {
		w.Tick()
	}
	want := refFrame([]Entity{{ID: 5, X: 3}})
	if !bytes.Equal(v.Out, want) {
		t.Fatalf("Out = % x, want % x — только кадры последнего тика", v.Out, want)
	}
}

// Без сущностей: каждому игроку один пустой кадр; без игроков — просто сдвиг.
func TestHiddenEmpty(t *testing.T) {
	w := &World{Viewers: []*Viewer{{}, {Radius: 3}}, MaxPerFrame: 4}
	w.Tick()
	for i, v := range w.Viewers {
		if !bytes.Equal(v.Out, []byte{0x02, 0x20, 0x00}) {
			t.Fatalf("игрок %d: Out = % x", i, v.Out)
		}
	}
	w2 := &World{Entities: []Entity{{VX: 2}}}
	w2.Tick()
	if w2.Entities[0].X != 2 {
		t.Fatalf("X = %v, want 2", w2.Entities[0].X)
	}
}

// Ровно кратное число сущностей: последний кадр не пустой и лишнего
// пустого кадра нет.
func TestHiddenSplitExactMultiple(t *testing.T) {
	var ents []Entity
	for i := range 6 {
		ents = append(ents, Entity{ID: int32(100 + i), Z: float64(i)})
	}
	v := &Viewer{Radius: 50}
	w := &World{Entities: ents, Viewers: []*Viewer{v}, MaxPerFrame: 3}
	w.Tick()
	if want := refOut(moved(ents), v, 3); !bytes.Equal(v.Out, want) {
		t.Fatalf("Out = % x\nwant  % x — два кадра по 3", v.Out, want)
	}
	w.MaxPerFrame = 1
	w.Tick()
	if want := refOut(moved(moved(ents)), v, 1); !bytes.Equal(v.Out, want) {
		t.Fatal("MaxPerFrame 1: по кадру на сущность")
	}
}

// Особые float64 кодируются битами как есть: -0, NaN, бесконечность.
func TestHiddenSpecialFloats(t *testing.T) {
	nan := math.NaN()
	negZero := math.Copysign(0, -1)
	ents := []Entity{{ID: 1, X: negZero, VX: negZero}, {ID: 2, X: 1, Z: math.Inf(1)}, {ID: 3, X: nan}}
	v := &Viewer{Radius: math.Inf(1)}
	w := &World{Entities: append([]Entity(nil), ents...), Viewers: []*Viewer{v}}
	w.Tick()
	// NaN не проходит сравнение «<= R²» — сущность 3 не видна;
	// (+Inf)² <= (+Inf)² — сущность 2 видна; -0 + -0 = -0.
	want := refOut(moved(ents), v, 0)
	if !bytes.Equal(v.Out, want) {
		t.Fatalf("Out = % x\nwant  % x", v.Out, want)
	}
}

// Ноль выделений: большой неподвижный мир, движущийся мир, где все
// всегда видимы, и мир с разбиением на кадры.
func TestHiddenNoAllocs(t *testing.T) {
	big := &World{MaxPerFrame: 0}
	for i := range 3000 {
		big.Entities = append(big.Entities, Entity{ID: int32(i*13 - 7000), X: float64(i % 60), Z: float64(i / 60)})
	}
	for i := range 64 {
		big.Viewers = append(big.Viewers, &Viewer{X: float64(i), Z: float64(i % 40), Radius: 16})
	}

	moving := &World{}
	for i := range 400 {
		moving.Entities = append(moving.Entities, Entity{ID: int32(i), X: float64(i % 20), VX: 0.01, VZ: -0.01})
	}
	for range 10 {
		moving.Viewers = append(moving.Viewers, &Viewer{Radius: 1e6})
	}

	split := &World{MaxPerFrame: 7}
	for i := range 300 {
		split.Entities = append(split.Entities, Entity{ID: -int32(i), X: float64(i % 30), Z: float64(i % 17)})
	}
	for i := range 12 {
		split.Viewers = append(split.Viewers, &Viewer{X: float64(i * 2), Z: 8, Radius: 9})
	}

	for name, w := range map[string]*World{"неподвижный": big, "движущийся": moving, "с разбиением": split} {
		w.Tick()
		w.Tick()
		if n := testing.AllocsPerRun(30, w.Tick); n != 0 {
			t.Errorf("мир %s: %v выделений на тик после прогрева, want 0", name, n)
		}
		for i, v := range w.Viewers {
			if !bytes.Equal(v.Out, refOut(w.Entities, v, w.MaxPerFrame)) {
				t.Fatalf("мир %s, игрок %d: кадры после прогрева не совпали с эталоном", name, i)
			}
		}
	}
}

// Отрицательный радиус действует как его модуль.
func TestHiddenNegativeRadius(t *testing.T) {
	w := &World{
		Entities: []Entity{{ID: 1, X: 3, Z: 4}, {ID: 2, X: 6}},
		Viewers:  []*Viewer{{Radius: -5}},
	}
	w.Tick()
	if want := refFrame([]Entity{{ID: 1, X: 3, Z: 4}}); !bytes.Equal(w.Viewers[0].Out, want) {
		t.Fatalf("Radius -5: Out = % x, want % x", w.Viewers[0].Out, want)
	}
}
