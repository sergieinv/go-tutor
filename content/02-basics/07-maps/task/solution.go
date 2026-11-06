package task

import (
	"maps"
	"slices"
	"strings"
)

// CountWords считает вхождения каждого слова в text без учёта регистра.
// Слова разделены пробельными символами. Всегда возвращает не-nil map.
func CountWords(text string) map[string]int {
	counts := make(map[string]int)
	for _, w := range strings.Fields(text) {
		// Для нового ключа m[k] равно 0, поэтому ++ сразу работает.
		counts[strings.ToLower(w)]++
	}
	return counts
}

// MostFrequent возвращает слова с наибольшим счётчиком по алфавиту.
// Для пустой или nil map возвращает пустой срез.
func MostFrequent(counts map[string]int) []string {
	best := 0
	words := []string{}
	// Обходим ключи в отсортированном порядке — тогда и результат
	// окажется отсортированным, без отдельной сортировки.
	for _, w := range slices.Sorted(maps.Keys(counts)) {
		switch c := counts[w]; {
		case len(words) == 0 || c > best:
			best = c
			words = append(words[:0], w)
		case c == best:
			words = append(words, w)
		}
	}
	return words
}
