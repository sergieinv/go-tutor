package task

import "testing"

func TestSwapNil(t *testing.T) {
	a := 5
	Swap(&a, nil)
	Swap(nil, &a)
	Swap(nil, nil)
	if a != 5 {
		t.Errorf("Swap с nil изменил значение: %d", a)
	}
}

func TestSwapSame(t *testing.T) {
	a := 7
	Swap(&a, &a)
	if a != 7 {
		t.Errorf("Swap(&a, &a) = %d, want 7", a)
	}
}

func TestMaxPtrPointsIntoSlice(t *testing.T) {
	s := []int{3, 9, 4}
	p := MaxPtr(s)
	if p != &s[1] {
		t.Fatalf("MaxPtr должен вернуть &s[1], а вернул указатель на копию")
	}
	*p = 0
	if s[1] != 0 {
		t.Errorf("запись через указатель не изменила срез: %v", s)
	}
}

func TestMaxPtrFirstOfEqual(t *testing.T) {
	s := []int{1, 8, 2, 8}
	if p := MaxPtr(s); p != &s[1] {
		t.Errorf("при равных максимумах ожидался указатель на первый (индекс 1)")
	}
}

func TestMaxPtrNegativeAndEmpty(t *testing.T) {
	s := []int{-5, -2, -9}
	if p := MaxPtr(s); p != &s[1] {
		t.Errorf("MaxPtr([-5 -2 -9]) должен указывать на -2")
	}
	if p := MaxPtr(nil); p != nil {
		t.Errorf("MaxPtr(nil) = %v, want nil", p)
	}
	if p := MaxPtr([]int{}); p != nil {
		t.Errorf("MaxPtr([]) = %v, want nil", p)
	}
}
