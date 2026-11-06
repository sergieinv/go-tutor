package task

import "testing"

func TestMinMaxEdges(t *testing.T) {
	tests := []struct {
		name   string
		in     []int
		lo, hi int
		ok     bool
	}{
		{"nil", nil, 0, 0, false},
		{"empty", []int{}, 0, 0, false},
		{"single", []int{7}, 7, 7, true},
		{"negative", []int{-3, -8, -1}, -8, -1, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lo, hi, ok := MinMax(tt.in)
			if lo != tt.lo || hi != tt.hi || ok != tt.ok {
				t.Errorf("MinMax(%v) = %d, %d, %v, want %d, %d, %v", tt.in, lo, hi, ok, tt.lo, tt.hi, tt.ok)
			}
		})
	}
}

func TestParsePointErrors(t *testing.T) {
	for _, in := range []string{"", "1", "1,2,3", "a,1", "1,b", "1,", ",1", "1.5,2", "5,9x"} {
		x, y, err := ParsePoint(in)
		if err == nil {
			t.Errorf("ParsePoint(%q) = %d, %d, nil, want ошибку", in, x, y)
		}
		if x != 0 || y != 0 {
			t.Errorf("ParsePoint(%q) при ошибке вернул %d, %d, want 0, 0", in, x, y)
		}
	}
}

func TestParsePointZeroAndSign(t *testing.T) {
	x, y, err := ParsePoint("0,+2")
	if err != nil || x != 0 || y != 2 {
		t.Errorf("ParsePoint(\"0,+2\") = %d, %d, %v, want 0, 2, nil", x, y, err)
	}
}
