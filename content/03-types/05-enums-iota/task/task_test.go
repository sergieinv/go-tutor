package task

import "testing"

func TestValues(t *testing.T) {
	if Read != 1 || Write != 2 || Exec != 4 {
		t.Fatalf("Read, Write, Exec = %d, %d, %d; want 1, 2, 4", Read, Write, Exec)
	}
}

func TestString(t *testing.T) {
	tests := []struct {
		p    Perm
		want string
	}{
		{Read | Write | Exec, "rwx"},
		{Read | Exec, "r-x"},
		{0, "---"},
	}
	for _, tt := range tests {
		if got := tt.p.String(); got != tt.want {
			t.Errorf("Perm(%d).String() = %q, want %q", uint8(tt.p), got, tt.want)
		}
	}
}

func TestParsePerm(t *testing.T) {
	p, err := ParsePerm("rw-")
	if err != nil || p != Read|Write {
		t.Errorf(`ParsePerm("rw-") = %d, %v; want %d, nil`, uint8(p), err, uint8(Read|Write))
	}
	if !p.Has(Read) || p.Has(Exec) {
		t.Errorf("Has: rw- должно иметь Read и не иметь Exec")
	}
}
