package task

import (
	"slices"
	"testing"
)

func TestPrimes(t *testing.T) {
	tests := []struct {
		n    int
		want []int
	}{
		{10, []int{2, 3, 5, 7}},
		{20, []int{2, 3, 5, 7, 11, 13, 17, 19}},
	}
	for _, tt := range tests {
		if got := Primes(tt.n); !slices.Equal(got, tt.want) {
			t.Errorf("Primes(%d) = %v, want %v", tt.n, got, tt.want)
		}
	}
}

func TestCollatzSteps(t *testing.T) {
	tests := []struct {
		n, want int
	}{
		{1, 0},
		{2, 1},
		{6, 8},
	}
	for _, tt := range tests {
		if got := CollatzSteps(tt.n); got != tt.want {
			t.Errorf("CollatzSteps(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}
