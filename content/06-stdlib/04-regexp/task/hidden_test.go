package task

import (
	"slices"
	"testing"
)

func TestExpandNotPlaceholders(t *testing.T) {
	vars := map[string]string{"1x": "BAD", "": "BAD", "name": "BAD", "a-b": "BAD", "a": "A"}
	tests := []struct {
		in, want string
	}{
		{"${1x}", "${1x}"},
		{"${}", "${}"},
		{"$name", "$name"},
		{"${a-b}", "${a-b}"},
		{"{a}", "{a}"},
		{"$${a}}", "$A}"},
		{"${ a }", "${ a }"},
	}
	for _, tt := range tests {
		if got := Expand(tt.in, vars); got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpandLiteralValues(t *testing.T) {
	vars := map[string]string{"price": "$1", "ref": "${other}", "other": "X", "empty": ""}
	tests := []struct {
		in, want string
	}{
		{"цена: ${price}", "цена: $1"},
		{"${ref}", "${other}"},
		{"[${empty}]", "[]"},
		{"${_under}", "${_under}"},
	}
	for _, tt := range tests {
		if got := Expand(tt.in, vars); got != tt.want {
			t.Errorf("Expand(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestExpandNilVars(t *testing.T) {
	if got := Expand("${a} ${b}", nil); got != "${a} ${b}" {
		t.Errorf("Expand с nil vars = %q, want без изменений", got)
	}
	if got := Expand("", map[string]string{"a": "1"}); got != "" {
		t.Errorf("Expand(\"\") = %q, want \"\"", got)
	}
}

func TestVarsEdge(t *testing.T) {
	if got := Vars("ничего $x ${1} ${}"); len(got) != 0 {
		t.Errorf("Vars без плейсхолдеров = %q, want пустой срез", got)
	}
	got := Vars("${_a}${B9}${_a}x${c_d}")
	want := []string{"_a", "B9", "c_d"}
	if !slices.Equal(got, want) {
		t.Errorf("Vars = %q, want %q", got, want)
	}
}
