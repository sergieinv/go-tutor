package task

import (
	"slices"
	"testing"
)

func TestExpand(t *testing.T) {
	vars := map[string]string{"name": "Анна", "city": "Казань", "n_2": "два"}
	tests := []struct {
		in, want string
	}{
		{"Привет, ${name}!", "Привет, Анна!"},
		{"${name} из ${city}", "Анна из Казань"},
		{"${n_2}${n_2}", "двадва"},
		{"${unknown} остаётся", "${unknown} остаётся"},
		{"без плейсхолдеров", "без плейсхолдеров"},
	}
	for _, tt := range tests {
		if got := Expand(tt.in, vars); got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestVars(t *testing.T) {
	got := Vars("${b} и ${a}, снова ${b}")
	want := []string{"b", "a"}
	if !slices.Equal(got, want) {
		t.Errorf("Vars = %q, want %q", got, want)
	}
}
