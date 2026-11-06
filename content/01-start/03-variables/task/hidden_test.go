package task

import "testing"

func TestFibEdges(t *testing.T) {
	tests := []struct {
		name    string
		n, want int
	}{
		{"zero", 0, 0},
		{"negative", -5, 0},
		{"twenty", 20, 6765},
		{"fifty", 50, 12586269025},
		{"ninety", 90, 2880067194370816120},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Fib(tt.n); got != tt.want {
				t.Errorf("Fib(%d) = %d, want %d", tt.n, got, tt.want)
			}
		})
	}
}

func TestGCDEdges(t *testing.T) {
	tests := []struct {
		name       string
		a, b, want int
	}{
		{"both zero", 0, 0, 0},
		{"second zero", 9, 0, 9},
		{"first zero", 0, 9, 9},
		{"negative first", -12, 18, 6},
		{"negative second", 12, -18, 6},
		{"both negative", -12, -18, 6},
		{"negative and zero", -7, 0, 7},
		{"equal", 21, 21, 21},
		{"big", 1071, 462, 21},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GCD(tt.a, tt.b); got != tt.want {
				t.Errorf("GCD(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
			}
		})
	}
}
