package task

import "testing"

func board(rows ...string) [3][3]byte {
	var b [3][3]byte
	for i, r := range rows {
		for j := range 3 {
			b[i][j] = r[j]
		}
	}
	return b
}

func TestRotate(t *testing.T) {
	in := board("XO.", "...", "..X")
	want := board("..X", "..O", "X..")
	if got := Rotate(in); got != want {
		t.Errorf("Rotate(%q) = %q, want %q", in, got, want)
	}
}

func TestWinner(t *testing.T) {
	tests := []struct {
		name string
		b    [3][3]byte
		want byte
	}{
		{"row", board("XXX", "OO.", "..."), 'X'},
		{"column", board("OX.", "OX.", "O.X"), 'O'},
		{"none", board("XO.", "...", "..."), '.'},
	}
	for _, tt := range tests {
		if got := Winner(tt.b); got != tt.want {
			t.Errorf("%s: Winner = %q, want %q", tt.name, got, tt.want)
		}
	}
}
