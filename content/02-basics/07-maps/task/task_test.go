package task

import (
	"maps"
	"slices"
	"testing"
)

func TestCountWords(t *testing.T) {
	got := CountWords("the cat and the hat")
	want := map[string]int{"the": 2, "cat": 1, "and": 1, "hat": 1}
	if !maps.Equal(got, want) {
		t.Errorf("CountWords = %v, want %v", got, want)
	}
}

func TestMostFrequent(t *testing.T) {
	tests := []struct {
		counts map[string]int
		want   []string
	}{
		{map[string]int{"go": 3, "c#": 1}, []string{"go"}},
		{map[string]int{"b": 2, "a": 2, "c": 1}, []string{"a", "b"}},
	}
	for _, tt := range tests {
		if got := MostFrequent(tt.counts); !slices.Equal(got, tt.want) {
			t.Errorf("MostFrequent(%v) = %v, want %v", tt.counts, got, tt.want)
		}
	}
}
