package task

import (
	"slices"
	"testing"
)

func TestPrimesEdges(t *testing.T) {
	tests := []struct {
		name string
		n    int
		want []int
	}{
		{"n=3", 3, []int{2}},
		{"n is prime, excluded", 7, []int{2, 3, 5}},
		{"squares of primes", 50, []int{2, 3, 5, 7, 11, 13, 17, 19, 23, 29, 31, 37, 41, 43, 47}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Primes(tt.n); !slices.Equal(got, tt.want) {
				t.Errorf("Primes(%d) = %v, want %v", tt.n, got, tt.want)
			}
		})
	}
}

func TestPrimesEmpty(t *testing.T) {
	for _, n := range []int{2, 1, 0, -5} {
		if got := Primes(n); len(got) != 0 {
			t.Errorf("Primes(%d) = %v, want пустой срез", n, got)
		}
	}
}

func TestPrimesCount(t *testing.T) {
	if got := len(Primes(1000)); got != 168 {
		t.Errorf("len(Primes(1000)) = %d, want 168", got)
	}
}

func TestCollatzStepsEdges(t *testing.T) {
	tests := []struct {
		n, want int
	}{
		{27, 111},
		{97, 118},
		{0, -1},
		{-4, -1},
	}
	for _, tt := range tests {
		if got := CollatzSteps(tt.n); got != tt.want {
			t.Errorf("CollatzSteps(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}
