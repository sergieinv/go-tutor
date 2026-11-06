package task

import "testing"

func TestStringHighBits(t *testing.T) {
	if got := Perm(0xF0 | 2).String(); got != "-w-" {
		t.Errorf("String() с лишними битами = %q, want %q", got, "-w-")
	}
}

func TestHasEdges(t *testing.T) {
	p := Read | Exec
	tests := []struct {
		q    Perm
		want bool
	}{
		{0, true},
		{Read, true},
		{Read | Exec, true},
		{Read | Write, false},
		{Write, false},
	}
	for _, tt := range tests {
		if got := p.Has(tt.q); got != tt.want {
			t.Errorf("r-x.Has(%s) = %v, want %v", tt.q, got, tt.want)
		}
	}
	if !Perm(0).Has(0) {
		t.Error("Perm(0).Has(0) = false, want true")
	}
}

func TestParseRoundTrip(t *testing.T) {
	for p := Perm(0); p < 8; p++ {
		got, err := ParsePerm(p.String())
		if err != nil || got != p {
			t.Errorf("ParsePerm(%q) = %d, %v; want %d, nil", p.String(), uint8(got), err, uint8(p))
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, s := range []string{"", "rw", "rwxr", "wrx", "r-X", "abc", "---x", "rw?"} {
		p, err := ParsePerm(s)
		if err == nil {
			t.Errorf("ParsePerm(%q) без ошибки, want ошибку", s)
		}
		if p != 0 {
			t.Errorf("ParsePerm(%q) при ошибке вернул %d, want 0", s, uint8(p))
		}
	}
}
