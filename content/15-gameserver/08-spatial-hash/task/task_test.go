package task

import (
	"slices"
	"testing"
)

func eqIDs(a, b []int) bool { return len(a) == 0 && len(b) == 0 || slices.Equal(a, b) }

func TestQueryRadiusBasic(t *testing.T) {
	g := NewGrid(16)
	g.Insert(1, 0, 0)
	g.Insert(2, 3, 4)   // расстояние 5
	g.Insert(3, 10, 10) // ~14.1
	g.Insert(4, 100, 100)
	cases := []struct {
		r    float64
		want []int
	}{
		{1, []int{1}},
		{5, []int{1, 2}},
		{15, []int{1, 2, 3}},
		{200, []int{1, 2, 3, 4}},
	}
	for _, c := range cases {
		if got := g.QueryRadius(0, 0, c.r, nil); !eqIDs(got, c.want) {
			t.Errorf("QueryRadius(0, 0, %v) = %v, want %v", c.r, got, c.want)
		}
	}
}

func TestMoveAndRemove(t *testing.T) {
	g := NewGrid(10)
	g.Insert(7, 5, 5)
	g.Insert(8, 6, 6)
	if !g.Move(7, 55, 55) {
		t.Fatal("Move(7) = false, want true")
	}
	if got := g.QueryRadius(5, 5, 3, nil); !eqIDs(got, []int{8}) {
		t.Fatalf("после Move старое место: %v, want [8]", got)
	}
	if got := g.QueryRadius(55, 55, 1, nil); !eqIDs(got, []int{7}) {
		t.Fatalf("после Move новое место: %v, want [7]", got)
	}
	if !g.Remove(8) || g.Len() != 1 {
		t.Fatalf("Remove(8): Len = %d, want 1", g.Len())
	}
	if got := g.QueryRadius(5, 5, 3, nil); len(got) != 0 {
		t.Fatalf("после Remove: %v, want пусто", got)
	}
}

func TestQueryAABB(t *testing.T) {
	g := NewGrid(8)
	g.Insert(1, -5, -5)
	g.Insert(2, 0, 0)
	g.Insert(3, 10, 2)
	g.Insert(4, 10, 20)
	if got := g.QueryAABB(-5, -5, 10, 10, nil); !eqIDs(got, []int{1, 2, 3}) {
		t.Fatalf("QueryAABB = %v, want [1 2 3]", got)
	}
}

func TestQueryAppendsToDst(t *testing.T) {
	g := NewGrid(4)
	g.Insert(5, 1, 1)
	g.Insert(3, 2, 2)
	got := g.QueryRadius(0, 0, 5, []int{100})
	if !slices.Equal(got, []int{100, 3, 5}) {
		t.Fatalf("QueryRadius с dst = %v, want [100 3 5]", got)
	}
}
