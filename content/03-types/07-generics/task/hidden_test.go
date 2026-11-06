package task

import (
	"slices"
	"testing"
)

func TestUniqEmpty(t *testing.T) {
	if got := Uniq[int](nil); len(got) != 0 {
		t.Errorf("Uniq(nil) = %v, want пустой срез", got)
	}
	if got := Uniq([]string{}); len(got) != 0 {
		t.Errorf("Uniq([]) = %v, want пустой срез", got)
	}
}

func TestUniqKeepsInput(t *testing.T) {
	in := []int{1, 1, 2, 2, 3}
	Uniq(in)
	if want := []int{1, 1, 2, 2, 3}; !slices.Equal(in, want) {
		t.Errorf("Uniq изменил вход: %v, want %v", in, want)
	}
}

type point struct{ X, Y int }

func TestUniqStructs(t *testing.T) {
	got := Uniq([]point{{1, 2}, {1, 2}, {2, 1}})
	if want := []point{{1, 2}, {2, 1}}; !slices.Equal(got, want) {
		t.Errorf("Uniq структур = %v, want %v", got, want)
	}
}

func TestGroupByEmpty(t *testing.T) {
	got := GroupBy([]int(nil), func(n int) bool { return n > 0 })
	if got == nil || len(got) != 0 {
		t.Errorf("GroupBy(nil) = %#v, want пустую не-nil map", got)
	}
}

func TestGroupByOrderAndKeyType(t *testing.T) {
	got := GroupBy([]int{5, 2, 8, 1, 4}, func(n int) bool { return n%2 == 0 })
	if !slices.Equal(got[true], []int{2, 8, 4}) || !slices.Equal(got[false], []int{5, 1}) {
		t.Errorf("GroupBy по чётности = %v, want map[false:[5 1] true:[2 8 4]]", got)
	}
}

func TestSetZeroValue(t *testing.T) {
	var s Set[point]
	if s.Has(point{}) || s.Len() != 0 {
		t.Errorf("пустой Set: Has=%v Len=%d", s.Has(point{}), s.Len())
	}
	s.Add(point{})
	if !s.Has(point{}) || s.Len() != 1 {
		t.Errorf("после Add(point{}): Has=%v Len=%d; want true 1", s.Has(point{}), s.Len())
	}
}
