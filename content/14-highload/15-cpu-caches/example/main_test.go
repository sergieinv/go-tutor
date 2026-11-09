package main

import (
	"sync"
	"testing"
)

// Тесты проверяют главное: раскладка не меняет результат.
func TestSameResult(t *testing.T) {
	vals := make([]int64, 1000)
	var want int64
	for i := range vals {
		vals[i] = int64(i * 3)
		want += vals[i]
	}
	got := []int64{
		sumSlice(vals),
		sumList(buildList(vals, sequential(len(vals)))),
		sumList(buildList(vals, shuffled(len(vals)))),
	}
	for _, g := range got {
		if g != want {
			t.Fatalf("суммы %v, want %d", got, want)
		}
	}
	t.Logf("сумма одинакова во всех раскладках: %d", want)
}

func TestMatrix(t *testing.T) {
	const n = 64
	m := make([]int64, n*n)
	for i := range m {
		m[i] = int64(i % 7)
	}
	if r, c := sumRows(m, n), sumCols(m, n); r != c {
		t.Fatalf("по строкам %d, по столбцам %d", r, c)
	}
	// Арифметика, а не измерение: сколько байт между соседними
	// обращениями при обходе по столбцам матрицы 2048×2048 int64.
	t.Logf("шаг по столбцу в матрице 2048×2048: %d байт — новая кеш-линия на каждый элемент", 2048*8)
}

// Данные для бенчмарков строятся один раз и только в бенчмарках:
// `go test -v` их не создаёт. 2 Ми значений — 16 МБ, а узлов списка — 32 МБ:
// больше L2 и обычно больше L3, приходящегося на одно ядро.
const big = 1 << 21

var data = sync.OnceValues(func() ([]int64, [2]*node) {
	vals := make([]int64, big)
	for i := range vals {
		vals[i] = int64(i)
	}
	return vals, [2]*node{buildList(vals, sequential(big)), buildList(vals, shuffled(big))}
})

var sink int64

func BenchmarkSlice(b *testing.B) {
	vals, _ := data()
	for b.Loop() {
		sink = sumSlice(vals)
	}
}

func BenchmarkListSequential(b *testing.B) {
	_, lists := data()
	for b.Loop() {
		sink = sumList(lists[0])
	}
}

func BenchmarkListShuffled(b *testing.B) {
	_, lists := data()
	for b.Loop() {
		sink = sumList(lists[1])
	}
}

const side = 2048

var matrix = sync.OnceValue(func() []int64 { return make([]int64, side*side) })

func BenchmarkMatrixRows(b *testing.B) {
	m := matrix()
	for b.Loop() {
		sink = sumRows(m, side)
	}
}

func BenchmarkMatrixCols(b *testing.B) {
	m := matrix()
	for b.Loop() {
		sink = sumCols(m, side)
	}
}
