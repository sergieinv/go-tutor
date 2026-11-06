package task

import (
	"slices"
	"testing"
)

func TestNilTree(t *testing.T) {
	if got := Sum(nil); got != 0 {
		t.Errorf("Sum(nil) = %d, want 0", got)
	}
	if got := Depth(nil); got != 0 {
		t.Errorf("Depth(nil) = %d, want 0", got)
	}
	if got := InOrder(nil); got == nil || len(got) != 0 {
		t.Errorf("InOrder(nil) = %#v, want пустой не-nil срез", got)
	}
}

func TestSingleNode(t *testing.T) {
	n := &Node{Val: -5}
	if Sum(n) != -5 || Depth(n) != 1 || !slices.Equal(InOrder(n), []int{-5}) {
		t.Errorf("один узел: Sum=%d Depth=%d InOrder=%v", Sum(n), Depth(n), InOrder(n))
	}
}

func TestUnbalanced(t *testing.T) {
	// Цепочка вправо: 1 → 2 → 3 → 4.
	var root *Node
	for v := 4; v >= 1; v-- {
		root = &Node{Val: v, Right: root}
	}
	if got := Depth(root); got != 4 {
		t.Errorf("Depth(цепочка из 4) = %d, want 4", got)
	}
	if got := InOrder(root); !slices.Equal(got, []int{1, 2, 3, 4}) {
		t.Errorf("InOrder(цепочка) = %v", got)
	}
	// Глубокое левое поддерево и мелкое правое.
	left := &Node{Val: 10, Left: &Node{Val: 5, Left: &Node{Val: 1}}, Right: &Node{Val: 20}}
	if got := Depth(left); got != 3 {
		t.Errorf("Depth = %d, want 3", got)
	}
	if got := InOrder(left); !slices.Equal(got, []int{1, 5, 10, 20}) {
		t.Errorf("InOrder = %v, want [1 5 10 20]", got)
	}
}

func TestDeepChain(t *testing.T) {
	// 100 000 уровней: стек горутины растёт, переполнения нет.
	var root *Node
	for range 100_000 {
		root = &Node{Val: 1, Left: root}
	}
	if got := Sum(root); got != 100_000 {
		t.Errorf("Sum(глубокая цепочка) = %d, want 100000", got)
	}
	if got := Depth(root); got != 100_000 {
		t.Errorf("Depth(глубокая цепочка) = %d, want 100000", got)
	}
}
