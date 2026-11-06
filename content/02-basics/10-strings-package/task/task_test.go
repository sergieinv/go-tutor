package task

import (
	"maps"
	"testing"
)

func TestParseKV(t *testing.T) {
	tests := []struct {
		in   string
		want map[string]int
	}{
		{"a=1;b=2", map[string]int{"a": 1, "b": 2}},
		{"a=1; b = 2;c=3", map[string]int{"a": 1, "b": 2, "c": 3}},
		{"x=-5", map[string]int{"x": -5}},
	}
	for _, tt := range tests {
		got, err := ParseKV(tt.in)
		if err != nil {
			t.Errorf("ParseKV(%q) вернул ошибку: %v", tt.in, err)
			continue
		}
		if !maps.Equal(got, tt.want) {
			t.Errorf("ParseKV(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseKVBadNumber(t *testing.T) {
	got, err := ParseKV("a=1;b=two")
	if err == nil {
		t.Errorf("ParseKV(%q) должен вернуть ошибку, получено %v", "a=1;b=two", got)
	}
}
