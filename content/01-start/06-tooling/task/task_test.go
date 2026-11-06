package task

import "testing"

func TestCountVerbs(t *testing.T) {
	tests := []struct {
		format string
		want   int
	}{
		{"hello", 0},
		{"%d items", 1},
		{"%s and %s", 2},
		{"%d%%", 1},
		{"[%-6s] [%6.2f]", 2},
		{"50%", -1},
	}
	for _, tt := range tests {
		if got := CountVerbs(tt.format); got != tt.want {
			t.Errorf("CountVerbs(%q) = %d, want %d", tt.format, got, tt.want)
		}
	}
}

func TestCheckCall(t *testing.T) {
	tests := []struct {
		format string
		nargs  int
		want   string
	}{
		{"%s: %d", 2, ""},
		{"%s and %s", 1, "format reads 2 args, but call has 1"},
		{"%d %", 1, "bad format"},
	}
	for _, tt := range tests {
		if got := CheckCall(tt.format, tt.nargs); got != tt.want {
			t.Errorf("CheckCall(%q, %d) = %q, want %q", tt.format, tt.nargs, got, tt.want)
		}
	}
}
