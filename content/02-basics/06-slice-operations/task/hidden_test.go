package task

import (
	"slices"
	"testing"
)

func TestWithoutKeepsSource(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	got := Without(s, 1)
	if want := []int{1, 2, 3, 4, 5}; !slices.Equal(s, want) {
		t.Fatalf("Without изменил исходный срез: %v, want %v", s, want)
	}
	got[0] = 100
	if s[0] != 1 {
		t.Errorf("результат Without делит память с исходным срезом")
	}
}

func TestWithoutFirst(t *testing.T) {
	if got := Without([]int{1, 2, 3}, 0); !slices.Equal(got, []int{2, 3}) {
		t.Errorf("Without([1 2 3], 0) = %v, want [2 3]", got)
	}
}

func TestWithoutOutOfRange(t *testing.T) {
	s := []int{1, 2, 3}
	for _, i := range []int{-1, 3, 100} {
		got := Without(s, i)
		if !slices.Equal(got, s) {
			t.Errorf("Without(%v, %d) = %v, want копию %v", s, i, got, s)
			continue
		}
		got[0] = -7
		if s[0] != 1 {
			t.Fatalf("Without(%v, %d) вернул не копию, а сам срез", s, i)
		}
	}
	if got := Without(nil, 0); len(got) != 0 {
		t.Errorf("Without(nil, 0) = %v, want пустой", got)
	}
}

func TestTopNKeepsSource(t *testing.T) {
	s := []int{5, 1, 9, 3, 7}
	got := TopN(s, 5)
	if want := []int{5, 1, 9, 3, 7}; !slices.Equal(s, want) {
		t.Fatalf("TopN изменил исходный срез: %v, want %v", s, want)
	}
	got[0] = -1
	if !slices.Contains(s, 9) {
		t.Errorf("результат TopN делит память с исходным срезом")
	}
}

func TestTopNEdges(t *testing.T) {
	tests := []struct {
		name string
		s    []int
		n    int
		want []int
	}{
		{"n greater than len", []int{3, 1, 2}, 10, []int{3, 2, 1}},
		{"duplicates", []int{4, 4, 1, 4}, 2, []int{4, 4}},
		{"negatives", []int{-5, -1, -3}, 2, []int{-1, -3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := TopN(tt.s, tt.n); !slices.Equal(got, tt.want) {
				t.Errorf("TopN(%v, %d) = %v, want %v", tt.s, tt.n, got, tt.want)
			}
		})
	}
}

func TestTopNEmpty(t *testing.T) {
	for _, n := range []int{0, -2} {
		if got := TopN([]int{1, 2}, n); got == nil || len(got) != 0 {
			t.Errorf("TopN([1 2], %d) = %#v, want пустой срез длины 0", n, got)
		}
	}
	if got := TopN(nil, 3); len(got) != 0 {
		t.Errorf("TopN(nil, 3) = %v, want пустой", got)
	}
}

func TestNeverNil(t *testing.T) {
	if got := Without(nil, 0); got == nil {
		t.Error("Without(nil, 0) вернул nil, ждали пустой срез")
	}
	if got := Without([]int{}, 5); got == nil {
		t.Error("Without([]int{}, 5) вернул nil, ждали пустой срез")
	}
	if got := TopN(nil, 3); got == nil {
		t.Error("TopN(nil, 3) вернул nil, ждали пустой срез")
	}
}
