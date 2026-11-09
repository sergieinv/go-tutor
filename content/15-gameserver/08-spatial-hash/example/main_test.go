package main

import (
	"fmt"
	"math/rand/v2"
	"slices"
	"testing"
)

// Мир 1024×1024 с центром в нуле: половина координат отрицательная.
// Зерно фиксировано — каждый прогон видит один и тот же мир.
func newWorld(n int, cell float64, seed uint64) (*Grid, *rand.Rand) {
	rng := rand.New(rand.NewPCG(seed, 1))
	g := NewGrid(cell)
	for id := range n {
		g.Insert(int32(id), rng.Float64()*1024-512, rng.Float64()*1024-512)
	}
	return g, rng
}

// Главный тест пространственного индекса — совпадение с полным
// перебором на случайных данных: перемещения внутри ячейки и через
// границу, телепорты, удаления и повторные вставки, разные радиусы
// и размеры ячейки.
func TestMatchesBruteForce(t *testing.T) {
	for _, cell := range []float64{4, 16, 64} {
		t.Run(fmt.Sprintf("cell=%v", cell), func(t *testing.T) {
			g, rng := newWorld(2000, cell, 42)
			removed := map[int32]bool{}
			var got, want []int32
			for round := range 30 {
				for id := range int32(2000) {
					switch p := rng.IntN(100); {
					case removed[id] && p < 20:
						g.Insert(id, rng.Float64()*1024-512, rng.Float64()*1024-512)
						delete(removed, id)
					case removed[id]:
					case p < 2:
						g.Remove(id)
						removed[id] = true
					case p < 4: // телепорт
						g.Move(id, rng.Float64()*1024-512, rng.Float64()*1024-512)
					default: // шаг до 2 блоков
						e := g.ents[id]
						g.Move(id, e.x+rng.Float64()*4-2, e.z+rng.Float64()*4-2)
					}
				}
				for range 20 {
					x, z := rng.Float64()*1100-550, rng.Float64()*1100-550
					r := rng.Float64() * 40
					got = g.QueryRadius(x, z, r, got[:0])
					want = g.bruteRadius(x, z, r, want[:0])
					if !slices.Equal(got, want) {
						t.Fatalf("раунд %d: QueryRadius(%.2f, %.2f, %.2f) = %v, перебор = %v", round, x, z, r, got, want)
					}
				}
			}
		})
	}
}

// Отрицательные координаты и граница ячейки — те же ловушки, что
// у чанков: (-0.5, -0.5) лежит в ячейке (-1,-1), но в радиус 1 от
// нуля попадает.
func TestNegativeAndBorder(t *testing.T) {
	g := NewGrid(16)
	g.Insert(0, -0.5, -0.5)
	g.Insert(1, 0.5, 0.5)
	g.Insert(2, 16, 0) // ровно на границе ячеек 0 и 1
	g.Insert(3, -3, 4) // ровно на расстоянии 5 от нуля
	cases := []struct {
		x, z, r float64
		want    []int32
	}{
		{0, 0, 1, []int32{0, 1}},
		{0, 0, 5, []int32{0, 1, 3}},
		{16, 0, 0, []int32{2}},
		{15.5, 0, 0.4999, nil},
		{0, 0, 1e9, []int32{0, 1, 2, 3}}, // радиус больше мира
	}
	for _, c := range cases {
		if got := g.QueryRadius(c.x, c.z, c.r, nil); !slices.Equal(got, c.want) {
			t.Errorf("QueryRadius(%v, %v, %v) = %v, want %v", c.x, c.z, c.r, got, c.want)
		}
	}
}

// Запрос в тике не должен аллоцировать: буфер результата передаётся
// снаружи и переиспользуется через `buf[:0]`.
func TestQueryDoesNotAllocate(t *testing.T) {
	g, _ := newWorld(10000, 16, 7)
	buf := make([]int32, 0, 256)
	allocs := testing.AllocsPerRun(100, func() {
		buf = g.QueryRadius(10, -20, 16, buf[:0])
	})
	fmt.Println("аллокаций на запрос:", allocs)
	if allocs != 0 {
		t.Fatalf("QueryRadius аллоцирует: %v", allocs)
	}
}

// Сколько сущностей посмотрел запрос: десятки вместо десяти тысяч.
func ExampleGrid_QueryRadius() {
	g, _ := newWorld(10000, 16, 7)
	found := g.QueryRadius(10, -20, 16, nil)
	fmt.Printf("найдено: %d, проверено: %d из 10000\n", len(found), g.Checked)
	// Output: найдено: 6, проверено: 19 из 10000
}

// Бенчмарк: тот же запрос полным перебором и сеткой с разным размером
// ячейки. Числа зависят от машины — смотрите на соотношение.
func BenchmarkQuery(b *testing.B) {
	run := func(b *testing.B, cell float64, brute bool) {
		g, rng := newWorld(10000, cell, 7)
		buf := make([]int32, 0, 1024)
		for b.Loop() {
			x, z := rng.Float64()*1024-512, rng.Float64()*1024-512
			if brute {
				buf = g.bruteRadius(x, z, 16, buf[:0])
			} else {
				buf = g.QueryRadius(x, z, 16, buf[:0])
			}
		}
	}
	b.Run("brute", func(b *testing.B) { run(b, 16, true) })
	for _, cell := range []float64{4, 16, 32, 128} {
		b.Run(fmt.Sprintf("cell=%v", cell), func(b *testing.B) { run(b, cell, false) })
	}
}

// Перемещение — самая частая операция; большинство шагов не меняет ячейку.
func BenchmarkMove(b *testing.B) {
	g, rng := newWorld(10000, 16, 7)
	id := int32(0)
	for b.Loop() {
		e := g.ents[id]
		g.Move(id, e.x+rng.Float64()-0.5, e.z+rng.Float64()-0.5)
		id = (id + 1) % 10000
	}
}
