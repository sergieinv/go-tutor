package task

import "testing"

func TestSum(t *testing.T) {
	if got := Sum([]int{1, 2, 3}); got != 6 {
		t.Errorf("Sum([1 2 3]) = %d, want 6", got)
	}
	if got := Sum([]float64{0.5, 1.5}); got != 2 {
		t.Errorf("Sum([0.5 1.5]) = %v, want 2", got)
	}
}

func TestClamp(t *testing.T) {
	tests := []struct{ v, lo, hi, want int }{
		{5, 0, 10, 5},
		{-3, 0, 10, 0},
		{42, 0, 10, 10},
	}
	for _, tt := range tests {
		if got := Clamp(tt.v, tt.lo, tt.hi); got != tt.want {
			t.Errorf("Clamp(%d, %d, %d) = %d, want %d", tt.v, tt.lo, tt.hi, got, tt.want)
		}
	}
}

func TestMaxBy(t *testing.T) {
	words := []string{"go", "kotlin", "c", "python"}
	got, ok := MaxBy(words, func(s string) int { return len(s) })
	if !ok || got != "kotlin" {
		t.Errorf("MaxBy по длине = %q, %v; want \"kotlin\", true", got, ok)
	}
}
