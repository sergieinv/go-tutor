package task

import "testing"

func TestReverse(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"abc", "cba"},
		{"Go, мир", "рим ,oG"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := Reverse(tt.in); got != tt.want {
			t.Errorf("Reverse(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		in   string
		n    int
		want string
	}{
		{"hello", 3, "hel"},
		{"привет", 3, "при"},
		{"go", 5, "go"},
	}
	for _, tt := range tests {
		if got := Truncate(tt.in, tt.n); got != tt.want {
			t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
		}
	}
}
