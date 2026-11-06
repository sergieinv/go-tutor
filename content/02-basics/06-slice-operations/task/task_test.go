package task

import (
	"slices"
	"testing"
)

func TestWithout(t *testing.T) {
	tests := []struct {
		s    []int
		i    int
		want []int
	}{
		{[]int{1, 2, 3, 4}, 1, []int{1, 3, 4}},
		{[]int{1, 2, 3, 4}, 3, []int{1, 2, 3}},
	}
	for _, tt := range tests {
		if got := Without(tt.s, tt.i); !slices.Equal(got, tt.want) {
			t.Errorf("Without(%v, %d) = %v, want %v", tt.s, tt.i, got, tt.want)
		}
	}
}

func TestTopN(t *testing.T) {
	tests := []struct {
		s    []int
		n    int
		want []int
	}{
		{[]int{5, 1, 9, 3, 7}, 3, []int{9, 7, 5}},
		{[]int{2, 8}, 1, []int{8}},
	}
	for _, tt := range tests {
		if got := TopN(tt.s, tt.n); !slices.Equal(got, tt.want) {
			t.Errorf("TopN(%v, %d) = %v, want %v", tt.s, tt.n, got, tt.want)
		}
	}
}
