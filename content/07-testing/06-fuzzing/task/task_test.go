package task

import (
	"slices"
	"testing"
)

func TestEscape(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "plain"},
		{"a,b", `a\,b`},
		{`C:\tmp`, `C:\\tmp`},
		{"two\nlines", `two\nlines`},
	}
	for _, tt := range tests {
		if got := Escape(tt.in); got != tt.want {
			t.Errorf("Escape(%q) = %q, want %q", tt.in, got, tt.want)
		}
		if got, err := Unescape(tt.want); err != nil || got != tt.in {
			t.Errorf("Unescape(%q) = %q, %v; want %q, nil", tt.want, got, err, tt.in)
		}
	}
}

func TestSplit(t *testing.T) {
	got, err := Split(`Иванов\, Иван,Москва`)
	want := []string{"Иванов, Иван", "Москва"}
	if err != nil || !slices.Equal(got, want) {
		t.Errorf("Split = %q, %v; want %q, nil", got, err, want)
	}
}

func TestRoundTrip(t *testing.T) {
	for _, s := range []string{"", "a,b", `\n`, `\`} {
		got, err := Unescape(Escape(s))
		if err != nil || got != s {
			t.Errorf("Unescape(Escape(%q)) = %q, %v", s, got, err)
		}
	}
}
