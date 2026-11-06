package task

import "testing"

func TestPushPop(t *testing.T) {
	var s Stack
	s.Push(1)
	s.Push(2)
	s.Push(3)
	if s.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", s.Len())
	}
	for _, want := range []int{3, 2, 1} {
		got, ok := s.Pop()
		if !ok || got != want {
			t.Fatalf("Pop() = %d, %v; want %d, true", got, ok, want)
		}
	}
}

func TestNewStackOrder(t *testing.T) {
	s := NewStack(10, 20, 30)
	if v, ok := s.Peek(); !ok || v != 30 {
		t.Errorf("Peek() = %d, %v; want 30, true", v, ok)
	}
	if s.Len() != 3 {
		t.Errorf("Len() = %d, want 3", s.Len())
	}
}
