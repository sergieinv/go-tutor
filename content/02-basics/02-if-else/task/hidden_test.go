package task

import "testing"

func TestBMICategoryBoundaries(t *testing.T) {
	// Рост 2 м: индекс = вес / 4, границы получаются точными.
	tests := []struct {
		weight, want string
	}{
		{"73.9", "underweight"},
		{"74", "normal"},
		{"99.9", "normal"},
		{"100", "overweight"},
		{"119.9", "overweight"},
		{"120", "obese"},
	}
	for _, tt := range tests {
		if got := BMICategory(tt.weight, "2"); got != tt.want {
			t.Errorf("BMICategory(%q, \"2\") = %q, want %q", tt.weight, got, tt.want)
		}
	}
}

func TestBMICategoryInvalid(t *testing.T) {
	tests := []struct {
		name, weight, height string
	}{
		{"empty weight", "", "1.8"},
		{"bad height", "70", "1,8"},
		{"zero height", "70", "0"},
		{"negative weight", "-70", "1.8"},
		{"zero weight", "0", "1.8"},
		{"negative height", "70", "-1.8"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := BMICategory(tt.weight, tt.height); got != "invalid" {
				t.Errorf("BMICategory(%q, %q) = %q, want \"invalid\"", tt.weight, tt.height, got)
			}
		})
	}
}
