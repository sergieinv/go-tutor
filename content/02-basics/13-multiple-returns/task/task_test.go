package task

import "testing"

func TestMinMax(t *testing.T) {
	lo, hi, ok := MinMax([]int{3, 1, 4, 1, 5})
	if lo != 1 || hi != 5 || !ok {
		t.Errorf("MinMax([3 1 4 1 5]) = %d, %d, %v, want 1, 5, true", lo, hi, ok)
	}
}

func TestParsePoint(t *testing.T) {
	tests := []struct {
		in   string
		x, y int
	}{
		{"3,-4", 3, -4},
		{" 10 , 20 ", 10, 20},
	}
	for _, tt := range tests {
		x, y, err := ParsePoint(tt.in)
		if err != nil || x != tt.x || y != tt.y {
			t.Errorf("ParsePoint(%q) = %d, %d, %v, want %d, %d, nil", tt.in, x, y, err, tt.x, tt.y)
		}
	}
}

func TestParsePointError(t *testing.T) {
	if _, _, err := ParsePoint("3;4"); err == nil {
		t.Error("ParsePoint(\"3;4\") должен вернуть ошибку")
	}
}
