package task

import (
	"slices"
	"testing"
)

func TestChunkBasic(t *testing.T) {
	got := Chunk([]int{1, 2, 3, 4, 5}, 2)
	want := [][]int{{1, 2}, {3, 4}, {5}}
	if !slices.EqualFunc(got, want, slices.Equal[[]int]) {
		t.Fatalf("Chunk([1 2 3 4 5], 2) = %v, want %v", got, want)
	}
}

func TestChunkAppendDoesNotLeak(t *testing.T) {
	s := []int{1, 2, 3, 4}
	parts := Chunk(s, 2)
	if len(parts) != 2 {
		t.Fatalf("Chunk([1 2 3 4], 2) вернул %d частей, want 2", len(parts))
	}
	_ = append(parts[0], 100)
	if parts[1][0] != 3 {
		t.Errorf("append к первой части изменил вторую: %v", parts[1])
	}
}
