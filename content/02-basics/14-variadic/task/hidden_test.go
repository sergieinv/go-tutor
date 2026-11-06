package task

import (
	"slices"
	"testing"
)

func TestSumEmpty(t *testing.T) {
	if got := Sum(); got != 0 {
		t.Errorf("Sum() = %d, want 0", got)
	}
	var s []int
	if got := Sum(s...); got != 0 {
		t.Errorf("Sum(nil...) = %d, want 0", got)
	}
}

func TestClampKeepsCallerSlice(t *testing.T) {
	s := []int{-1, 5, 20}
	got := Clamp(0, 10, s...)
	if want := []int{0, 5, 10}; !slices.Equal(got, want) {
		t.Errorf("Clamp(0, 10, s...) = %v, want %v", got, want)
	}
	if want := []int{-1, 5, 20}; !slices.Equal(s, want) {
		t.Errorf("Clamp изменил срез вызывающего кода: %v, want %v", s, want)
	}
	got[0] = 99
	if s[0] != -1 {
		t.Errorf("результат Clamp делит массив с аргументом")
	}
}

func TestClampEmpty(t *testing.T) {
	if got := Clamp(1, 2); got == nil || len(got) != 0 {
		t.Errorf("Clamp(1, 2) = %#v, want пустой не-nil срез", got)
	}
}

func TestClampEqualBounds(t *testing.T) {
	got := Clamp(3, 3, 1, 3, 9)
	if want := []int{3, 3, 3}; !slices.Equal(got, want) {
		t.Errorf("Clamp(3, 3, 1, 3, 9) = %v, want %v", got, want)
	}
}

func TestClampPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("Clamp(5, 1, ...) должен паниковать")
		}
	}()
	Clamp(5, 1, 3)
}
