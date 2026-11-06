package task

import "testing"

func TestEmpty(t *testing.T) {
	var s Stack
	if v, ok := s.Pop(); ok || v != 0 {
		t.Errorf("Pop() на пустом стеке = %d, %v; want 0, false", v, ok)
	}
	if v, ok := s.Peek(); ok || v != 0 {
		t.Errorf("Peek() на пустом стеке = %d, %v; want 0, false", v, ok)
	}
	if s.Len() != 0 {
		t.Errorf("Len() = %d, want 0", s.Len())
	}
	if n := NewStack().Len(); n != 0 {
		t.Errorf("NewStack().Len() = %d, want 0", n)
	}
}

func TestPeekDoesNotPop(t *testing.T) {
	s := NewStack(1)
	s.Peek()
	s.Peek()
	if s.Len() != 1 {
		t.Errorf("Peek изменил стек: Len() = %d, want 1", s.Len())
	}
}

func TestNewStackDoesNotAlias(t *testing.T) {
	src := []int{1, 2, 3}
	s := NewStack(src...)
	src[2] = 100
	if v, _ := s.Peek(); v != 3 {
		t.Errorf("изменение исходного среза видно в стеке: Peek() = %d, want 3", v)
	}

	// Push не должен писать в массив среза вызывающего.
	src = []int{1, 2, 3}
	s = NewStack(src[:2]...)
	s.Push(9)
	if src[2] != 3 {
		t.Errorf("Push записал в срез вызывающего: %v", src)
	}
}

func TestPopThenPush(t *testing.T) {
	s := NewStack(1, 2)
	s.Pop()
	s.Push(5)
	got := []int{}
	for s.Len() > 0 {
		v, _ := s.Pop()
		got = append(got, v)
	}
	if len(got) != 2 || got[0] != 5 || got[1] != 1 {
		t.Errorf("после Pop и Push стек = %v, want [5 1]", got)
	}
}
