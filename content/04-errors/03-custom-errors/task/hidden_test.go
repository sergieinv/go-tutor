package task

import (
	"errors"
	"testing"
)

// Ловушка урока: nil-указатель внутри error — не nil.
func TestCheckReturnsRealNil(t *testing.T) {
	var err error = Check("{[()]}")
	if err != nil {
		t.Fatalf("Check вернул не-nil интерфейс %#v для корректной строки", err)
	}
}

func TestCheckEdges(t *testing.T) {
	tests := []struct {
		s    string
		want BracketError
	}{
		{")", BracketError{Pos: 0, Char: ')'}},
		{"(()", BracketError{Pos: 3}},
		{"([)]", BracketError{Pos: 2, Char: ')'}},
		{"ok} (", BracketError{Pos: 2, Char: '}'}},
		{"привет)", BracketError{Pos: 12, Char: ')'}},
		{"{ж", BracketError{Pos: 3}},
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

func TestCheckIgnoresOtherChars(t *testing.T) {
	for _, s := range []string{"abc", "<>", "привет", "f(x) = [y]"} {
		if err := Check(s); err != nil {
			t.Errorf("Check(%q) = %v, want nil", s, err)
		}
	}
}

func TestBracketErrorTextRune(t *testing.T) {
	if got := (&BracketError{Pos: 0, Char: '}'}).Error(); got != "unexpected '}' at 0" {
		t.Errorf("Error() = %q, want %q", got, "unexpected '}' at 0")
	}
}
