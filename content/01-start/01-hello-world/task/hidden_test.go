package task

import (
	"strings"
	"testing"
)

func TestBannerEdges(t *testing.T) {
	tests := []struct {
		name  string
		title string
		want  string
	}{
		{"empty", "", "+--+\n|  |\n+--+"},
		{"one char", "x", "+---+\n| x |\n+---+"},
		{"spaces kept", " a b ", "+-------+\n|  a b  |\n+-------+"},
		{"long", "Hello, World!", "+---------------+\n| Hello, World! |\n+---------------+"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Banner(tt.title); got != tt.want {
				t.Errorf("Banner(%q):\n%s\nwant:\n%s", tt.title, got, tt.want)
			}
		})
	}
}

func TestBannerNoTrailingNewline(t *testing.T) {
	got := Banner("Hi")
	if strings.HasSuffix(got, "\n") {
		t.Errorf("Banner(%q) заканчивается переводом строки: %q", "Hi", got)
	}
	if n := strings.Count(got, "\n"); n != 2 {
		t.Errorf("Banner(%q) содержит %d переводов строки, want 2", "Hi", n)
	}
}
