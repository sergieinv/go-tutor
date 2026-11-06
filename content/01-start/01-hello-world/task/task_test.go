package task

import "testing"

func TestBanner(t *testing.T) {
	tests := []struct {
		title string
		want  string
	}{
		{"Hello", "+-------+\n| Hello |\n+-------+"},
		{"Go", "+----+\n| Go |\n+----+"},
	}
	for _, tt := range tests {
		t.Run(tt.title, func(t *testing.T) {
			if got := Banner(tt.title); got != tt.want {
				t.Errorf("Banner(%q):\n%s\nwant:\n%s", tt.title, got, tt.want)
			}
		})
	}
}
