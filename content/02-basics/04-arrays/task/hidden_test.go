package task

import "testing"

func TestRotateFourTimes(t *testing.T) {
	in := board("XOX", "O.O", "..X")
	got := in
	for range 4 {
		got = Rotate(got)
	}
	if got != in {
		t.Errorf("четыре поворота должны вернуть исходное поле: %q, want %q", got, in)
	}
}

func TestRotateKeepsInput(t *testing.T) {
	in := board("XO.", "...", "..X")
	saved := in
	_ = Rotate(in)
	if in != saved {
		t.Errorf("Rotate изменил исходное поле: %q", in)
	}
}

func TestRotateFull(t *testing.T) {
	in := board("XXO", "O.X", "OOX")
	want := board("OOX", "O.X", "XXO")
	if got := Rotate(in); got != want {
		t.Errorf("Rotate(%q) = %q, want %q", in, got, want)
	}
}

func TestWinnerEdges(t *testing.T) {
	tests := []struct {
		name string
		b    [3][3]byte
		want byte
	}{
		{"main diagonal", board("X.O", ".XO", "..X"), 'X'},
		{"anti diagonal", board("X.O", ".OX", "O.."), 'O'},
		{"empty board", board("...", "...", "..."), '.'},
		{"last row", board("XX.", "O.X", "OOO"), 'O'},
		{"last column", board("O.X", "O.X", ".OX"), 'X'},
		{"full draw", board("XOX", "XOO", "OXX"), '.'},
	}
	for _, tt := range tests {
		if got := Winner(tt.b); got != tt.want {
			t.Errorf("%s: Winner = %q, want %q", tt.name, got, tt.want)
		}
	}
}
