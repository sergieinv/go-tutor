package task

import "testing"

func TestPercent(t *testing.T) {
	tests := []struct {
		part, total int
		want        float64
	}{
		{1, 2, 50},
		{1, 3, 33.3},
		{2, 3, 66.7},
		{3, 4, 75},
	}
	for _, tt := range tests {
		if got := Percent(tt.part, tt.total); got != tt.want {
			t.Errorf("Percent(%d, %d) = %v, want %v", tt.part, tt.total, got, tt.want)
		}
	}
}

func TestAddSat(t *testing.T) {
	tests := []struct {
		a, b, want uint8
	}{
		{1, 2, 3},
		{100, 55, 155},
		{200, 100, 255},
	}
	for _, tt := range tests {
		if got := AddSat(tt.a, tt.b); got != tt.want {
			t.Errorf("AddSat(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
