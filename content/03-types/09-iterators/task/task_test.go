package task

import (
	"slices"
	"testing"
)

func TestFilter(t *testing.T) {
	even := func(n int) bool { return n%2 == 0 }
	got := slices.Collect(Filter(slices.Values([]int{1, 2, 3, 4, 5, 6}), even))
	if want := []int{2, 4, 6}; !slices.Equal(got, want) {
		t.Errorf("Filter = %v, want %v", got, want)
	}
}

func TestTakeFib(t *testing.T) {
	got := slices.Collect(Take(Fib(), 8))
	if want := []int{0, 1, 1, 2, 3, 5, 8, 13}; !slices.Equal(got, want) {
		t.Errorf("Take(Fib(), 8) = %v, want %v", got, want)
	}
}
