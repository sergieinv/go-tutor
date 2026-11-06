package task

import "testing"

func TestFib(t *testing.T) {
	tests := []struct{ n, want int }{
		{1, 1},
		{2, 1},
		{3, 2},
		{10, 55},
	}
	for _, tt := range tests {
		if got := Fib(tt.n); got != tt.want {
			t.Errorf("Fib(%d) = %d, want %d", tt.n, got, tt.want)
		}
	}
}

func TestGCD(t *testing.T) {
	tests := []struct{ a, b, want int }{
		{12, 18, 6},
		{18, 12, 6},
		{7, 5, 1},
	}
	for _, tt := range tests {
		if got := GCD(tt.a, tt.b); got != tt.want {
			t.Errorf("GCD(%d, %d) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}
