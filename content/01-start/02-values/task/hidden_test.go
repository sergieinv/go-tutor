package task

import "testing"

func TestPercentEdges(t *testing.T) {
	tests := []struct {
		name        string
		part, total int
		want        float64
	}{
		{"zero total", 5, 0, 0},
		{"negative total", 5, -10, 0},
		{"zero part", 0, 7, 0},
		{"more than whole", 150, 100, 150},
		{"negative part", -1, 4, -25},
		{"one sixth", 1, 6, 16.7},
		{"one eighth", 1, 8, 12.5},
		{"one seventh", 1, 7, 14.3},
		{"whole", 7, 7, 100},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Percent(tt.part, tt.total); got != tt.want {
				t.Errorf("Percent(%d, %d) = %v, want %v", tt.part, tt.total, got, tt.want)
			}
		})
	}
}

func TestAddSatEdges(t *testing.T) {
	tests := []struct {
		name       string
		a, b, want uint8
	}{
		{"zeros", 0, 0, 0},
		{"max plus zero", 255, 0, 255},
		{"exactly max", 200, 55, 255},
		{"one over", 200, 56, 255},
		{"half plus half", 128, 128, 255},
		{"max plus max", 255, 255, 255},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := AddSat(tt.a, tt.b); got != tt.want {
				t.Errorf("AddSat(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
