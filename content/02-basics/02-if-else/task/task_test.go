package task

import "testing"

func TestBMICategory(t *testing.T) {
	tests := []struct {
		weight, height, want string
	}{
		{"50", "1.80", "underweight"},
		{"70", "1.75", "normal"},
		{"85", "1.75", "overweight"},
		{"110", "1.70", "obese"},
		{"abc", "1.70", "invalid"},
	}
	for _, tt := range tests {
		if got := BMICategory(tt.weight, tt.height); got != tt.want {
			t.Errorf("BMICategory(%q, %q) = %q, want %q", tt.weight, tt.height, got, tt.want)
		}
	}
}
