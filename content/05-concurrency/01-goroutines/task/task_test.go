package task

import (
	"slices"
	"testing"
)

func TestParallelMapSquares(t *testing.T) {
	tests := []struct {
		name string
		in   []int
		want []int
	}{
		{"один", []int{3}, []int{9}},
		{"несколько", []int{1, 2, 3, 4}, []int{1, 4, 9, 16}},
		{"отрицательные", []int{-2, 0, 5}, []int{4, 0, 25}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParallelMap(tt.in, func(x int) int { return x * x })
			if !slices.Equal(got, tt.want) {
				t.Errorf("ParallelMap(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParallelMapEmpty(t *testing.T) {
	calls := 0
	got := ParallelMap(nil, func(x int) int { calls++; return x })
	if len(got) != 0 || calls != 0 {
		t.Errorf("ParallelMap(nil) = %v, вызовов f: %d; want пустой срез и 0 вызовов", got, calls)
	}
}
