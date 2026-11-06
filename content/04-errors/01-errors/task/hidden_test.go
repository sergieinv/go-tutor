package task

import (
	"strings"
	"testing"
)

func TestParseConfigMoreErrors(t *testing.T) {
	tests := []struct {
		name   string
		lines  []string
		prefix string
	}{
		{"empty key", []string{"", "=5"}, "line 2: "},
		{"duplicate", []string{"a=1", "b=2", "a=3"}, "line 3: "},
		{"first error wins", []string{"x", "y"}, "line 1: "},
		{"blank lines count", []string{"", "  ", "bad"}, "line 3: "},
		{"empty value", []string{"a="}, "line 1: "},
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

func TestParseConfigEmpty(t *testing.T) {
	for _, in := range [][]string{nil, {}, {"", "   "}} {
		got, err := ParseConfig(in)
		if err != nil {
			t.Errorf("ParseConfig(%q) вернул ошибку %v", in, err)
		}
		if got == nil || len(got) != 0 {
			t.Errorf("ParseConfig(%q) = %#v, want пустую не-nil map", in, got)
		}
	}
}

func TestParseConfigNegative(t *testing.T) {
	got, err := ParseConfig([]string{"delta=-3"})
	if err != nil || got["delta"] != -3 {
		t.Errorf("ParseConfig(delta=-3) = (%v, %v), want map[delta:-3]", got, err)
	}
}
