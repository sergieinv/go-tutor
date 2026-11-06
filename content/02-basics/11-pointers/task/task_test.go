package task

import "testing"

func TestSwap(t *testing.T) {
	a, b := 1, 2
	Swap(&a, &b)
	if a != 2 || b != 1 {
		t.Errorf("после Swap a=%d b=%d, want a=2 b=1", a, b)
	}
}

func TestMaxPtr(t *testing.T) {
	s := []int{3, 9, 4}
	p := MaxPtr(s)
	if p == nil {
		t.Fatal("MaxPtr([3 9 4]) = nil")
	}
	if *p != 9 {
		t.Errorf("*MaxPtr([3 9 4]) = %d, want 9", *p)
	}
}
