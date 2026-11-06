package task

import (
	"slices"
	"testing"
)

func TestSum(t *testing.T) {
	if got := Sum(1, 2, 3); got != 6 {
		t.Errorf("Sum(1, 2, 3) = %d, want 6", got)
	}
	s := []int{10, -4}
	if got := Sum(s...); got != 6 {
		t.Errorf("Sum(s...) = %d, want 6", got)
	}
}

func TestClamp(t *testing.T) {
	got := Clamp(0, 10, -5, 3, 15)
	if want := []int{0, 3, 10}; !slices.Equal(got, want) {
		t.Errorf("Clamp(0, 10, -5, 3, 15) = %v, want %v", got, want)
	}
}
