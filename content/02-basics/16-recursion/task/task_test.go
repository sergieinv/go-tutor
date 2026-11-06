package task

import (
	"slices"
	"testing"
)

// sample — дерево 4(2(1, 3), 6).
func sample() *Node {
	return &Node{4,
		&Node{2, &Node{1, nil, nil}, &Node{3, nil, nil}},
		&Node{6, nil, nil},
	}
}

func TestSum(t *testing.T) {
	if got := Sum(sample()); got != 16 {
		t.Errorf("Sum = %d, want 16", got)
	}
}

func TestDepth(t *testing.T) {
	if got := Depth(sample()); got != 3 {
		t.Errorf("Depth = %d, want 3", got)
	}
}

func TestInOrder(t *testing.T) {
	got := InOrder(sample())
	if want := []int{1, 2, 3, 4, 6}; !slices.Equal(got, want) {
		t.Errorf("InOrder = %v, want %v", got, want)
	}
}
