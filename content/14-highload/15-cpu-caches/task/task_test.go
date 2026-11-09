package task

import (
	"slices"
	"testing"
)

func TestFIFO(t *testing.T) {
	var q Queue
	for i := range int64(5) {
		q.Push(i * 10)
	}
	if got := slices.Collect(q.All()); !slices.Equal(got, []int64{0, 10, 20, 30, 40}) {
		t.Fatalf("All() = %v, want [0 10 20 30 40]", got)
	}
	for want := range int64(5) {
		if v, ok := q.Pop(); v != want*10 || !ok {
			t.Fatalf("Pop() = %d, %v; want %d, true", v, ok, want*10)
		}
	}
	if v, ok := q.Pop(); v != 0 || ok || q.Len() != 0 {
		t.Fatalf("Pop() на пустой = %d, %v (Len %d); want 0, false (0)", v, ok, q.Len())
	}
}

var sinkV int64

func TestPushNoAllocs(t *testing.T) {
	var q Queue
	// Разогрев: очередь один раз дорастает до рабочего размера.
	for i := range int64(100) {
		q.Push(i)
	}
	for range 100 {
		q.Pop()
	}
	allocs := testing.AllocsPerRun(100, func() {
		for i := range int64(50) {
			q.Push(i)
		}
		for range 50 {
			sinkV, _ = q.Pop()
		}
	})
	if allocs != 0 {
		t.Errorf("50 Push + 50 Pop после разогрева: %v выделений, want 0", allocs)
	}
}
