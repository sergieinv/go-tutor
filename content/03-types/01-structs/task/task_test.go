package task

import "testing"

func TestNormalize(t *testing.T) {
	tests := []struct {
		name string
		in   Rect
		want Rect
	}{
		{"already", Rect{Point{0, 0}, Point{2, 3}}, Rect{Point{0, 0}, Point{2, 3}}},
		{"swapped", Rect{Point{2, 3}, Point{0, 0}}, Rect{Point{0, 0}, Point{2, 3}}},
		{"mixed", Rect{Point{0, 3}, Point{2, 0}}, Rect{Point{0, 0}, Point{2, 3}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Normalize(tt.in); got != tt.want {
				t.Errorf("Normalize(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestGrow(t *testing.T) {
	r := Rect{Point{1, 1}, Point{3, 4}}
	Grow(&r, 2)
	if want := (Rect{Point{-1, -1}, Point{5, 6}}); r != want {
		t.Errorf("после Grow(&r, 2) r = %v, want %v", r, want)
	}
}

func TestOverlap(t *testing.T) {
	tests := []struct {
		name   string
		a, b   Rect
		want   Rect
		wantOK bool
	}{
		{"partial", Rect{Point{0, 0}, Point{4, 4}}, Rect{Point{2, 2}, Point{6, 6}}, Rect{Point{2, 2}, Point{4, 4}}, true},
		{"disjoint", Rect{Point{0, 0}, Point{1, 1}}, Rect{Point{5, 5}, Point{6, 6}}, Rect{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Overlap(tt.a, tt.b)
			if got != tt.want || ok != tt.wantOK {
				t.Errorf("Overlap(%v, %v) = %v, %v; want %v, %v", tt.a, tt.b, got, ok, tt.want, tt.wantOK)
			}
		})
	}
}
