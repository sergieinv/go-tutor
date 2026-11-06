package task

import (
	"slices"
	"testing"
)

func TestMap(t *testing.T) {
	double := func(x int) int { return x * 2 }
	got := Map([]int{1, 2, 3}, double)
	if want := []int{2, 4, 6}; !slices.Equal(got, want) {
		t.Errorf("Map([1 2 3], double) = %v, want %v", got, want)
	}
}

func TestReduce(t *testing.T) {
	sum := func(acc, x int) int { return acc + x }
	if got := Reduce([]int{1, 2, 3, 4}, 0, sum); got != 10 {
		t.Errorf("Reduce(sum) = %d, want 10", got)
	}
	mul := func(acc, x int) int { return acc * x }
	if got := Reduce([]int{2, 3}, 1, mul); got != 6 {
		t.Errorf("Reduce(mul) = %d, want 6", got)
	}
}
