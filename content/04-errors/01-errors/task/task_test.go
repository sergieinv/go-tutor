package task

import (
	"maps"
	"strings"
	"testing"
)

func TestParseConfigOK(t *testing.T) {
	got, err := ParseConfig([]string{"port=8080", "", " workers = 4 "})
	if err != nil {
		t.Fatalf("ParseConfig вернул ошибку: %v", err)
	}
	want := map[string]int{"port": 8080, "workers": 4}
	if !maps.Equal(got, want) {
		t.Errorf("ParseConfig = %v, want %v", got, want)
	}
}

func TestParseConfigErrors(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		prefix string
	}{
		{"no equals", []string{"a=1", "oops"}, "line 2: "},
		{"not a number", []string{"a=x"}, "line 1: "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseConfig(tt.lines)
			if err == nil {
				t.Fatalf("ParseConfig(%q) не вернул ошибку", tt.lines)
			}
			if !strings.HasPrefix(err.Error(), tt.prefix) {
				t.Errorf("текст ошибки %q должен начинаться с %q", err, tt.prefix)
			}
			if got != nil {
				t.Errorf("при ошибке map должна быть nil, получено %v", got)
			}
		})
	}
}
