package task

import (
	"math/rand/v2"
	"slices"
	"testing"
)

func TestHiddenZeroValue(t *testing.T) {
	var q Queue
	if q.Len() != 0 {
		t.Errorf("Len() нулевой очереди = %d", q.Len())
	}
	if v, ok := q.Peek(); v != 0 || ok {
		t.Errorf("Peek() пустой = %d, %v; want 0, false", v, ok)
	}
	if v, ok := q.Pop(); v != 0 || ok {
		t.Errorf("Pop() пустой = %d, %v; want 0, false", v, ok)
	}
	for v := range q.All() {
		t.Errorf("All() пустой выдал %d", v)
	}
}

func TestHiddenPeek(t *testing.T) {
	var q Queue
	q.Push(7)
	q.Push(8)
	for range 3 {
		if v, ok := q.Peek(); v != 7 || !ok {
			t.Fatalf("Peek() = %d, %v; want 7, true", v, ok)
		}
	}
	if q.Len() != 2 {
		t.Fatalf("Peek убрал элемент: Len() = %d", q.Len())
	}
	q.Pop()
	if v, ok := q.Peek(); v != 8 || !ok {
		t.Fatalf("после Pop Peek() = %d, %v; want 8, true", v, ok)
	}
}

// Рост очереди, когда элементы уже «перешли через край» буфера.
func TestHiddenGrowAfterWrap(t *testing.T) {
	var q Queue
	var model []int64
	next := int64(0)
	for round := range 6 {
		for range 5 + round*7 {
			q.Push(next)
			model = append(model, next)
			next++
		}
		for range 3 + round*4 {
			v, ok := q.Pop()
			if !ok || v != model[0] {
				t.Fatalf("раунд %d: Pop() = %d, %v; want %d, true", round, v, ok, model[0])
			}
			model = model[1:]
		}
		if got := slices.Collect(q.All()); !slices.Equal(got, model) {
			t.Fatalf("раунд %d: All() = %v, want %v", round, got, model)
		}
		if q.Len() != len(model) {
			t.Fatalf("раунд %d: Len() = %d, want %d", round, q.Len(), len(model))
		}
	}
}

func TestHiddenRandomOps(t *testing.T) {
	r := rand.New(rand.NewPCG(14, 15))
	var q Queue
	var model []int64
	for i := range 20_000 {
		switch op := r.IntN(10); {
		case op < 6:
			v := r.Int64()
			q.Push(v)
			model = append(model, v)
		case op < 9:
			v, ok := q.Pop()
			if len(model) == 0 {
				if ok || v != 0 {
					t.Fatalf("шаг %d: Pop() пустой = %d, %v", i, v, ok)
				}
				continue
			}
			if !ok || v != model[0] {
				t.Fatalf("шаг %d: Pop() = %d, %v; want %d, true", i, v, ok, model[0])
			}
			model = model[1:]
		default:
			if q.Len() != len(model) {
				t.Fatalf("шаг %d: Len() = %d, want %d", i, q.Len(), len(model))
			}
		}
	}
	if got := slices.Collect(q.All()); !slices.Equal(got, model) {
		t.Fatalf("в конце All() расходится с моделью: %d элементов против %d", len(got), len(model))
	}
}

func TestHiddenAllBreak(t *testing.T) {
	var q Queue
	for i := range int64(10) {
		q.Push(i)
	}
	var got []int64
	for v := range q.All() {
		got = append(got, v)
		if v == 3 {
			break
		}
	}
	if !slices.Equal(got, []int64{0, 1, 2, 3}) {
		t.Fatalf("All() с break на 3 = %v", got)
	}
	if q.Len() != 10 {
		t.Fatalf("All() изменил очередь: Len() = %d", q.Len())
	}
}

func TestHiddenSteadyStateNoAllocs(t *testing.T) {
	var q Queue
	for i := range int64(1000) {
		q.Push(i)
	}
	for range 990 {
		q.Pop()
	}
	// В очереди 10 элементов, голова далеко от начала буфера.
	allocs := testing.AllocsPerRun(200, func() {
		for i := range int64(500) {
			q.Push(i)
		}
		for range 500 {
			sinkV, _ = q.Pop()
		}
	})
	if allocs != 0 {
		t.Errorf("500 Push + 500 Pop при рабочем размере 1000: %v выделений, want 0", allocs)
	}
	var sum int64
	if a := testing.AllocsPerRun(100, func() {
		for v := range q.All() {
			sum += v
		}
	}); a != 0 {
		t.Errorf("обход All(): %v выделений, want 0", a)
	}
	_ = sum
}

func TestHiddenPushAllocsAreFew(t *testing.T) {
	// Рост — не чаще, чем при удвоении: на 100 000 Push в пустую очередь —
	// десятки выделений, а не по одному на элемент.
	allocs := testing.AllocsPerRun(5, func() {
		var q Queue
		for i := range int64(100_000) {
			q.Push(i)
		}
	})
	if allocs > 64 {
		t.Errorf("100 000 Push в новую очередь: %v выделений, want не больше 64 (рост с запасом, а не узел на элемент)", allocs)
	}
}
