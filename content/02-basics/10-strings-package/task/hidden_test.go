package task

import (
	"maps"
	"testing"
)

func TestParseKVEdges(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want map[string]int
	}{
		{"trailing separator", "a=1;", map[string]int{"a": 1}},
		{"empty pairs", " ; a=1 ;; ", map[string]int{"a": 1}},
		{"case folding", "A=1; a=2", map[string]int{"a": 2}},
		{"spaces everywhere", "  key  =  42  ", map[string]int{"key": 42}},
		{"plus sign", "n=+7", map[string]int{"n": 7}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseKV(tt.in)
			if err != nil {
				t.Fatalf("ParseKV(%q) вернул ошибку: %v", tt.in, err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("ParseKV(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseKVEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", ";;"} {
		got, err := ParseKV(in)
		if err != nil {
			t.Errorf("ParseKV(%q) вернул ошибку: %v", in, err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("ParseKV(%q) = %#v, want пустую не-nil map", in, got)
		}
	}
}

func TestParseKVErrors(t *testing.T) {
	for _, in := range []string{"a", "a=1;b", "=5", " = 5", "a=", "a=1.5", "a=1=2", "a=12x"} {
		got, err := ParseKV(in)
		if err == nil {
			t.Errorf("ParseKV(%q) = %v, want ошибку", in, got)
		}
		if got != nil {
			t.Errorf("ParseKV(%q) при ошибке вернул %v, want nil", in, got)
		}
	}
}
