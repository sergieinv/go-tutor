package task

import (
	"slices"
	"strconv"
	"testing"
)

func TestMapOrder(t *testing.T) {
	tests := []struct {
		name    string
		in      []int
		workers int
		want    []string
	}{
		{"один воркер", []int{1, 2, 3}, 1, []string{"1", "2", "3"}},
		{"три воркера", []int{5, 4, 3, 2, 1, 0}, 3, []string{"5", "4", "3", "2", "1", "0"}},
		{"воркеров больше задач", []int{7, 8}, 10, []string{"7", "8"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Map(tt.in, tt.workers, strconv.Itoa)
			if !slices.Equal(got, tt.want) {
				t.Errorf("Map(%v, %d) = %v, want %v", tt.in, tt.workers, got, tt.want)
			}
		})
	}
}

func TestMapManyItems(t *testing.T) {
	in := make([]int, 1000)
	for i := range in {
		in[i] = i
	}
	got := Map(in, 8, func(x int) int { return x * x })
	for i, v := range got {
		if v != i*i {
			t.Fatalf("out[%d] = %d, want %d", i, v, i*i)
		}
	}
}
