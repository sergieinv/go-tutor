package task

import (
	"slices"
	"testing"
)

func TestSamplerDropsWhenFull(t *testing.T) {
	s := NewSampler(2)
	var accepted []bool
	for i := 1; i <= 4; i++ {
		accepted = append(accepted, s.Offer(i))
	}
	if want := []bool{true, true, false, false}; !slices.Equal(accepted, want) {
		t.Fatalf("Offer вернул %v, want %v", accepted, want)
	}
	if got := s.Drain(); !slices.Equal(got, []int{1, 2}) {
		t.Fatalf("Drain() = %v, want [1 2]", got)
	}
}

func TestSamplerTryTake(t *testing.T) {
	s := NewSampler(3)
	if v, ok := s.TryTake(); ok || v != 0 {
		t.Fatalf("TryTake() на пустом = (%d, %v), want (0, false)", v, ok)
	}
	s.Offer(10)
	s.Offer(20)
	if v, ok := s.TryTake(); !ok || v != 10 {
		t.Fatalf("TryTake() = (%d, %v), want (10, true)", v, ok)
	}
	if got := s.Drain(); !slices.Equal(got, []int{20}) {
		t.Fatalf("Drain() = %v, want [20]", got)
	}
}

func TestSamplerDrainEmpty(t *testing.T) {
	s := NewSampler(3)
	if got := s.Drain(); len(got) != 0 {
		t.Fatalf("Drain() на пустом = %v, want пустой срез", got)
	}
}
