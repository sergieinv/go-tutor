package task

import (
	"slices"
	"testing"
)

func TestUniq(t *testing.T) {
	got := Uniq([]int{3, 1, 3, 2, 1})
	if want := []int{3, 1, 2}; !slices.Equal(got, want) {
		t.Errorf("Uniq = %v, want %v", got, want)
	}
	gotS := Uniq([]string{"a", "b", "a"})
	if want := []string{"a", "b"}; !slices.Equal(gotS, want) {
		t.Errorf("Uniq = %v, want %v", gotS, want)
	}
}

func TestGroupBy(t *testing.T) {
	words := []string{"go", "rust", "gleam", "ruby", "c"}
	got := GroupBy(words, func(w string) string { return w[:1] })
	want := map[string][]string{"g": {"go", "gleam"}, "r": {"rust", "ruby"}, "c": {"c"}}
	if len(got) != len(want) {
		t.Fatalf("GroupBy вернул %d групп, want %d: %v", len(got), len(want), got)
	}
	for k, w := range want {
		if !slices.Equal(got[k], w) {
			t.Errorf("группа %q = %v, want %v", k, got[k], w)
		}
	}
}

func TestSet(t *testing.T) {
	var s Set[string]
	s.Add("a")
	s.Add("b")
	s.Add("a")
	if s.Len() != 2 || !s.Has("a") || s.Has("c") {
		t.Errorf("Set после Add(a, b, a): Len=%d Has(a)=%v Has(c)=%v; want 2 true false", s.Len(), s.Has("a"), s.Has("c"))
	}
}
