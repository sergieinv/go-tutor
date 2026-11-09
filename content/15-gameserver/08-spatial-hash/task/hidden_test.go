package task

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// Модель «в лоб»: id → координаты, запрос — полный перебор.
type hPoint struct{ x, z float64 }

func hRadius(m map[int]hPoint, x, z, r float64) []int {
	var out []int
	for id, p := range m {
		if dx, dz := p.x-x, p.z-z; dx*dx+dz*dz <= r*r {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func hAABB(m map[int]hPoint, minX, minZ, maxX, maxZ float64) []int {
	var out []int
	for id, p := range m {
		if p.x >= minX && p.x <= maxX && p.z >= minZ && p.z <= maxZ {
			out = append(out, id)
		}
	}
	slices.Sort(out)
	return out
}

func TestHiddenRandomAgainstBruteForce(t *testing.T) {
	for _, cell := range []float64{0.5, 3, 16, 50} {
		t.Run(fmt.Sprint("cell=", cell), func(t *testing.T) {
			rng := rand.New(rand.NewPCG(8, uint64(cell*10)))
			g := NewGrid(cell)
			m := map[int]hPoint{}
			coord := func() float64 { return rng.Float64()*400 - 200 }
			// id — разреженные и отрицательные тоже.
			ids := make([]int, 300)
			for i := range ids {
				ids[i] = rng.IntN(1_000_000) - 500_000
			}
			for step := range 6000 {
				id := ids[rng.IntN(len(ids))]
				switch op := rng.IntN(10); {
				case op < 3:
					p := hPoint{coord(), coord()}
					g.Insert(id, p.x, p.z)
					m[id] = p
				case op < 7:
					p, ok := m[id]
					np := hPoint{p.x + rng.Float64()*6 - 3, p.z + rng.Float64()*6 - 3}
					if op == 6 {
						np = hPoint{coord(), coord()}
					}
					if got := g.Move(id, np.x, np.z); got != ok {
						t.Fatalf("шаг %d: Move(%d) = %v, want %v", step, id, got, ok)
					}
					if ok {
						m[id] = np
					}
				case op < 8:
					_, ok := m[id]
					if got := g.Remove(id); got != ok {
						t.Fatalf("шаг %d: Remove(%d) = %v, want %v", step, id, got, ok)
					}
					delete(m, id)
				default:
					x, z := rng.Float64()*440-220, rng.Float64()*440-220
					r := rng.Float64() * 30
					if got, want := g.QueryRadius(x, z, r, nil), hRadius(m, x, z, r); !eqIDs(got, want) {
						t.Fatalf("шаг %d: QueryRadius(%v, %v, %v) = %v, want %v", step, x, z, r, got, want)
					}
					w, h := rng.Float64()*40, rng.Float64()*40
					if got, want := g.QueryAABB(x, z, x+w, z+h, nil), hAABB(m, x, z, x+w, z+h); !eqIDs(got, want) {
						t.Fatalf("шаг %d: QueryAABB(%v, %v, %v, %v) = %v, want %v", step, x, z, x+w, z+h, got, want)
					}
				}
				if g.Len() != len(m) {
					t.Fatalf("шаг %d: Len = %d, want %d", step, g.Len(), len(m))
				}
			}
		})
	}
}

func TestHiddenNegativeCellsAndBorders(t *testing.T) {
	g := NewGrid(16)
	g.Insert(1, -0.5, -0.5)
	g.Insert(2, 0.5, 0.5)
	g.Insert(3, -16, 0)    // ровно на границе ячеек -1 и -2
	g.Insert(4, -16.01, 0) // уже в ячейке -2
	g.Insert(5, 0, -6)     // ровно на расстоянии 6 от нуля
	cases := []struct {
		x, z, r float64
		want    []int
	}{
		{0, 0, 1, []int{1, 2}},
		{0, 0, 6, []int{1, 2, 5}},
		{0, 0, 5.999, []int{1, 2}},
		{-16, 0, 0, []int{3}},
		{-16, 0, 0.02, []int{3, 4}},
		{-15.99, 0, 0.015, []int{3}},
	}
	for _, c := range cases {
		if got := g.QueryRadius(c.x, c.z, c.r, nil); !eqIDs(got, c.want) {
			t.Errorf("QueryRadius(%v, %v, %v) = %v, want %v", c.x, c.z, c.r, got, c.want)
		}
	}
	if got := g.QueryAABB(-16, -6, 0.5, 0.5, nil); !eqIDs(got, []int{1, 2, 3, 5}) {
		t.Errorf("QueryAABB с включёнными границами = %v, want [1 2 3 5]", got)
	}
}

func TestHiddenEmptyQueries(t *testing.T) {
	g := NewGrid(4)
	if got := g.QueryRadius(0, 0, 10, nil); len(got) != 0 {
		t.Fatalf("пустая сетка: %v", got)
	}
	g.Insert(1, 0, 0)
	if got := g.QueryRadius(0, 0, -1, nil); len(got) != 0 {
		t.Fatalf("r < 0: %v, want пусто", got)
	}
	if got := g.QueryRadius(0, 0, 0, nil); !eqIDs(got, []int{1}) {
		t.Fatalf("r = 0 в точке сущности: %v, want [1]", got)
	}
	if got := g.QueryAABB(1, -1, -1, 1, nil); len(got) != 0 {
		t.Fatalf("minX > maxX: %v, want пусто", got)
	}
	if got := g.QueryAABB(-1, 1, 1, -1, nil); len(got) != 0 {
		t.Fatalf("minZ > maxZ: %v, want пусто", got)
	}
	if got := g.QueryAABB(0, 0, 0, 0, nil); !eqIDs(got, []int{1}) {
		t.Fatalf("вырожденный прямоугольник в точке: %v, want [1]", got)
	}
}

// Огромный радиус: обход всех ячеек квадрата запроса не закончится
// (и переполнит int) — нужно переключиться на непустые ячейки.
func TestHiddenHugeQuery(t *testing.T) {
	g := NewGrid(0.5)
	for i := range 50 {
		g.Insert(i, float64(i*1000-25000), float64(-i*700))
	}
	got := g.QueryRadius(0, 0, 1e12, nil)
	if len(got) != 50 || got[0] != 0 || got[49] != 49 {
		t.Fatalf("QueryRadius(r = 1e12): %d сущностей, want 50 по порядку", len(got))
	}
	if got := g.QueryAABB(-1e12, -1e12, 1e12, 1e12, nil); len(got) != 50 {
		t.Fatalf("QueryAABB на 1e12: %d, want 50", len(got))
	}
	if got := g.QueryAABB(-1e12, -1e12, 1e12, -1e5, nil); len(got) != 0 {
		t.Fatalf("QueryAABB огромный, но мимо всех: %v", got)
	}
}

func TestHiddenInsertExistingMoves(t *testing.T) {
	g := NewGrid(10)
	g.Insert(1, 0, 0)
	g.Insert(1, 100, 100)
	if g.Len() != 1 {
		t.Fatalf("повторный Insert: Len = %d, want 1", g.Len())
	}
	if got := g.QueryRadius(0, 0, 1, nil); len(got) != 0 {
		t.Fatalf("повторный Insert оставил старое место: %v", got)
	}
	if got := g.QueryRadius(100, 100, 1, nil); !eqIDs(got, []int{1}) {
		t.Fatalf("повторный Insert: новое место %v, want [1]", got)
	}
	if g.Move(2, 0, 0) || g.Len() != 1 {
		t.Fatal("Move неизвестного id должен вернуть false и не добавлять сущность")
	}
	if g.Remove(2) {
		t.Fatal("Remove неизвестного id = true")
	}
	if !g.Remove(1) || g.Remove(1) {
		t.Fatal("Remove(1) дважды: want true, затем false")
	}
	if got := g.QueryRadius(100, 100, 1e6, nil); len(got) != 0 {
		t.Fatalf("после Remove: %v", got)
	}
}

func TestHiddenSameCellManyEntities(t *testing.T) {
	// Много сущностей в одной ячейке и удаление из середины её списка.
	g := NewGrid(100)
	for i := range 20 {
		g.Insert(i, float64(i), 0)
	}
	for _, id := range []int{0, 7, 19, 8, 3} {
		g.Remove(id)
	}
	g.Move(10, 50, 50)  // в той же ячейке
	g.Move(11, 150, 50) // в соседней
	want := []int{1, 2, 4, 5, 6, 9, 12, 13, 14, 15, 16, 17, 18}
	if got := g.QueryRadius(0, 0, 30, nil); !eqIDs(got, want) {
		t.Fatalf("QueryRadius = %v, want %v", got, want)
	}
	if got := g.QueryAABB(0, 0, 200, 60, nil); len(got) != 15 {
		t.Fatalf("QueryAABB: %v, want 15 сущностей", got)
	}
}

func TestHiddenNoAllocations(t *testing.T) {
	g := NewGrid(16)
	rng := rand.New(rand.NewPCG(1, 2))
	for i := range 5000 {
		g.Insert(i, rng.Float64()*1000-500, rng.Float64()*1000-500)
	}
	buf := make([]int, 0, 1024)
	if a := testing.AllocsPerRun(50, func() { buf = g.QueryRadius(3, -7, 24, buf[:0]) }); a != 0 {
		t.Errorf("QueryRadius с запасом ёмкости в dst аллоцирует: %v", a)
	}
	if a := testing.AllocsPerRun(50, func() { buf = g.QueryAABB(-30, -30, 30, 30, buf[:0]) }); a != 0 {
		t.Errorf("QueryAABB с запасом ёмкости в dst аллоцирует: %v", a)
	}
	g.Insert(9999, 1, 1)
	flip := false
	if a := testing.AllocsPerRun(50, func() {
		flip = !flip
		if flip {
			g.Move(9999, 2, 2)
		} else {
			g.Move(9999, 1, 1)
		}
	}); a != 0 {
		t.Errorf("Move внутри ячейки аллоцирует: %v", a)
	}
}
