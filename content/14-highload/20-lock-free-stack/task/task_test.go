package task

import (
	"slices"
	"sync"
	"testing"
)

func TestPushPop(t *testing.T) {
	var s Stack[int]
	s.Push(1)
	s.Push(2)
	s.Push(3)
	for _, want := range []int{3, 2, 1} {
		if v, ok := s.Pop(); !ok || v != want {
			t.Fatalf("Pop() = %d, %v; want %d, true", v, ok, want)
		}
	}
	if v, ok := s.Pop(); ok || v != 0 {
		t.Fatalf("Pop() на пустом = %d, %v; want 0, false", v, ok)
	}
}

func TestPushManyDrain(t *testing.T) {
	var s Stack[string]
	s.Push("a")
	s.PushMany("b", "c", "d")
	if got := s.Drain(); !slices.Equal(got, []string{"d", "c", "b", "a"}) {
		t.Fatalf("Drain() = %q, want [d c b a]", got)
	}
	if got := s.Drain(); got != nil {
		t.Fatalf("Drain() на пустом = %q, want nil", got)
	}
}

func TestConcurrentPush(t *testing.T) {
	var s Stack[int]
	var wg sync.WaitGroup
	for g := range 4 {
		wg.Go(func() {
			for i := range 500 {
				s.Push(g*500 + i)
			}
		})
	}
	wg.Wait()
	got := s.Drain()
	slices.Sort(got)
	if len(got) != 2000 {
		t.Fatalf("len(Drain()) = %d, want 2000", len(got))
	}
	for i, v := range got {
		if v != i {
			t.Fatalf("значение %d потеряно или повторено", i)
		}
	}
}
