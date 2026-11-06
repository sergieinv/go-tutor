package task

import (
	"testing"
	"unicode/utf8"
)

func TestReverseMultibyte(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"я", "я"},
		{"a😀б", "б😀a"},
		{"日本語", "語本日"},
	}
	for _, tt := range tests {
		got := Reverse(tt.in)
		if got != tt.want {
			t.Errorf("Reverse(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if !utf8.ValidString(got) {
			t.Errorf("Reverse(%q) вернул некорректный UTF-8 — переворачивались байты, а не руны", tt.in)
		}
	}
}

func TestTruncateEdges(t *testing.T) {
	tests := []struct {
		name string
		in   string
		n    int
		want string
	}{
		{"zero", "мир", 0, ""},
		{"negative", "мир", -2, ""},
		{"exact", "мир", 3, "мир"},
		{"empty", "", 2, ""},
		{"emoji", "😀😀😀", 2, "😀😀"},
		{"mixed", "Go, мир", 5, "Go, м"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Truncate(tt.in, tt.n)
			if got != tt.want {
				t.Errorf("Truncate(%q, %d) = %q, want %q", tt.in, tt.n, got, tt.want)
			}
			if !utf8.ValidString(got) {
				t.Errorf("Truncate разрезал многобайтовый символ: %q", got)
			}
		})
	}
}
