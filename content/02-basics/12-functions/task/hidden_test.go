package task

import (
	"slices"
	"testing"
)

func TestMapDoesNotModifyInput(t *testing.T) {
	in := []int{1, 2, 3}
	out := Map(in, func(x int) int { return -x })
	if !slices.Equal(in, []int{1, 2, 3}) {
		t.Errorf("Map изменил исходный срез: %v", in)
	}
	out[0] = 100
	if in[0] != 1 {
		t.Errorf("результат Map делит массив с исходным срезом")
	}
}

func TestMapEmpty(t *testing.T) {
	for _, in := range [][]int{nil, {}} {
		got := Map(in, func(x int) int { return x })
		if got == nil || len(got) != 0 {
			t.Errorf("Map(%#v) = %#v, want пустой не-nil срез", in, got)
		}
	}
	if got := Map(nil, nil); got == nil || len(got) != 0 {
		t.Errorf("Map(nil, nil) = %#v, want пустой не-nil срез", got)
	}
}

func TestMapNilFunc(t *testing.T) {
	in := []int{4, 5}
	got := Map(in, nil)
	if !slices.Equal(got, in) {
		t.Errorf("Map(%v, nil) = %v, want копию", in, got)
	}
	got[0] = 0
	if in[0] != 4 {
		t.Errorf("Map(s, nil) вернул сам s, а не копию")
	}
}

func TestReduceOrderAndEdges(t *testing.T) {
	// Вычитание некоммутативно: проверяем порядок слева направо.
	sub := func(acc, x int) int { return acc - x }
	if got := Reduce([]int{1, 2, 3}, 10, sub); got != 4 {
		t.Errorf("Reduce(sub, 10) = %d, want 4", got)
	}
	digits := func(acc, x int) int { return acc*10 + x }
	if got := Reduce([]int{1, 2, 3}, 0, digits); got != 123 {
		t.Errorf("Reduce(digits) = %d, want 123", got)
	}
	if got := Reduce(nil, 7, sub); got != 7 {
		t.Errorf("Reduce(nil, 7) = %d, want 7", got)
	}
	if got := Reduce([]int{1, 2}, 7, nil); got != 7 {
		t.Errorf("Reduce с f == nil = %d, want 7", got)
	}
}
