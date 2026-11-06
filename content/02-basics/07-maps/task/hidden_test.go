package task

import (
	"maps"
	"slices"
	"testing"
)

func TestCountWordsCaseAndSpaces(t *testing.T) {
	got := CountWords("  Go\tgo\nGO  rust ")
	want := map[string]int{"go": 3, "rust": 1}
	if !maps.Equal(got, want) {
		t.Errorf("CountWords = %v, want %v", got, want)
	}
}

func TestCountWordsEmptyIsWritable(t *testing.T) {
	for _, text := range []string{"", "   \n\t"} {
		got := CountWords(text)
		if got == nil {
			t.Fatalf("CountWords(%q) вернул nil map", text)
		}
		if len(got) != 0 {
			t.Errorf("CountWords(%q) = %v, want пустую map", text, got)
		}
		got["x"]++ // запись в nil-map запаниковала бы
	}
}

func TestMostFrequentEmpty(t *testing.T) {
	for _, m := range []map[string]int{nil, {}} {
		got := MostFrequent(m)
		if got == nil || len(got) != 0 {
			t.Errorf("MostFrequent(%v) = %#v, want пустой срез длины 0", m, got)
		}
	}
}

func TestMostFrequentTiesSorted(t *testing.T) {
	counts := map[string]int{"pear": 4, "apple": 4, "fig": 4, "kiwi": 1, "zucchini": 4}
	want := []string{"apple", "fig", "pear", "zucchini"}
	// Порядок обхода map случаен — прогоняем несколько раз.
	for range 20 {
		if got := MostFrequent(counts); !slices.Equal(got, want) {
			t.Fatalf("MostFrequent = %v, want %v", got, want)
		}
	}
}

func TestMostFrequentKeepsInput(t *testing.T) {
	counts := map[string]int{"a": 1, "b": 5}
	_ = MostFrequent(counts)
	if want := map[string]int{"a": 1, "b": 5}; !maps.Equal(counts, want) {
		t.Errorf("MostFrequent изменил map: %v", counts)
	}
}

func TestPipeline(t *testing.T) {
	got := MostFrequent(CountWords("To be or not to be"))
	if want := []string{"be", "to"}; !slices.Equal(got, want) {
		t.Errorf("MostFrequent(CountWords(...)) = %v, want %v", got, want)
	}
}
