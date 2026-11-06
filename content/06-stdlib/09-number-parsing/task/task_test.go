package task

import (
	"errors"
	"strconv"
	"testing"
)

func TestParseSize(t *testing.T) {
	tests := []struct {
		in   string
		want int64
	}{
		{"512", 512},
		{"10K", 10 * 1024},
		{"3M", 3 * 1024 * 1024},
		{"2G", 2 * 1024 * 1024 * 1024},
		{"0", 0},
	}
	for _, tt := range tests {
		got, err := ParseSize(tt.in)
		if err != nil || got != tt.want {
			t.Errorf("ParseSize(%q) = %d, %v; want %d, nil", tt.in, got, err, tt.want)
		}
	}
}

func TestParseSizeSyntax(t *testing.T) {
	for _, in := range []string{"", "abc", "-5", "10k"} {
		got, err := ParseSize(in)
		if !errors.Is(err, strconv.ErrSyntax) || got != 0 {
			t.Errorf("ParseSize(%q) = %d, %v; want 0 и ErrSyntax", in, got, err)
		}
	}
}
