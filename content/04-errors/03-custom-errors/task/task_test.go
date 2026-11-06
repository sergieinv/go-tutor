package task

import (
	"errors"
	"testing"
)

func TestCheckValid(t *testing.T) {
	for _, s := range []string{"", "()", "a[b{c}d]e", "([]{})"} {
		if err := Check(s); err != nil {
			t.Errorf("Check(%q) = %v, want nil", s, err)
		}
	}
}

func TestCheckInvalid(t *testing.T) {
	tests := []struct {
		s    string
		want BracketError
	}{
		{"(]", BracketError{Pos: 1, Char: ']'}},
		{"((", BracketError{Pos: 2}},
	}
	for _, tt := range tests {
		err := Check(tt.s)
		var be *BracketError
		if !errors.As(err, &be) {
			t.Fatalf("Check(%q) = %v, want *BracketError", tt.s, err)
		}
		if *be != tt.want {
			t.Errorf("Check(%q) = %+v, want %+v", tt.s, *be, tt.want)
		}
	}
}

func TestBracketErrorText(t *testing.T) {
	if got := (&BracketError{Pos: 3, Char: ']'}).Error(); got != "unexpected ']' at 3" {
		t.Errorf("Error() = %q, want %q", got, "unexpected ']' at 3")
	}
	if got := (&BracketError{Pos: 5}).Error(); got != "unexpected end at 5" {
		t.Errorf("Error() = %q, want %q", got, "unexpected end at 5")
	}
}
