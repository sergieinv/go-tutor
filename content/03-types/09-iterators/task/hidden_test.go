package task

import (
	"iter"
	"slices"
	"testing"
)

// counting возвращает итератор 0..limit-1 и счётчик выданных элементов.
func counting(limit int) (iter.Seq[int], *int) {
	n := 0
	return func(yield func(int) bool) {
		for i := range limit {
			n++
			if !yield(i) {
				return
			}
		}
	}, &n
}

func TestTakeDoesNotOverpull(t *testing.T) {
	seq, pulled := counting(100)
	got := slices.Collect(Take(seq, 3))
	if !slices.Equal(got, []int{0, 1, 2}) {
		t.Errorf("Take(seq, 3) = %v, want [0 1 2]", got)
	}
	if *pulled != 3 {
		t.Errorf("Take(seq, 3) запросил у seq %d элементов, want 3", *pulled)
	}
}

func TestTakeNonPositive(t *testing.T) {
	for _, n := range []int{0, -1} {
		seq, pulled := counting(10)
		if got := slices.Collect(Take(seq, n)); len(got) != 0 {
			t.Errorf("Take(seq, %d) = %v, want пусто", n, got)
		}
		if *pulled != 0 {
			t.Errorf("Take(seq, %d) вызвал seq: выдано %d элементов", n, *pulled)
		}
	}
}

func TestTakeShortSource(t *testing.T) {
	got := slices.Collect(Take(slices.Values([]string{"a", "b"}), 5))
	if !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("Take(короткий, 5) = %v, want [a b]", got)
	}
}

func TestEarlyBreakNoPanic(t *testing.T) {
	// Если адаптер игнорирует результат yield, рантайм паникует.
	positive := func(n int) bool { return n > 0 }
	for v := range Filter(Fib(), positive) {
		if v > 10 {
			break
		}
	}
	for v := range Take(Fib(), 50) {
		if v == 3 {
			break
		}
	}
	for v := range Fib() {
		if v > 100 {
			break
		}
	}
}

func TestReusable(t *testing.T) {
	seq := Take(Filter(Fib(), func(n int) bool { return n%2 == 0 }), 4)
	first := slices.Collect(seq)
	second := slices.Collect(seq)
	want := []int{0, 2, 8, 34}
	if !slices.Equal(first, want) || !slices.Equal(second, want) {
		t.Errorf("повторный обход: %v и %v, want %v оба раза", first, second, want)
	}
}

func TestFilterNone(t *testing.T) {
	got := slices.Collect(Filter(slices.Values([]int{1, 3}), func(int) bool { return false }))
	if len(got) != 0 {
		t.Errorf("Filter без подходящих = %v, want пусто", got)
	}
}
