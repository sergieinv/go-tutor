package task

import "testing"

func TestNormalizeKeepsInput(t *testing.T) {
	in := Rect{Point{5, 5}, Point{1, 1}}
	_ = Normalize(in)
	if want := (Rect{Point{5, 5}, Point{1, 1}}); in != want {
		t.Errorf("Normalize изменил аргумент: %v, want %v", in, want)
	}
}

func TestGrowNil(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("Grow(nil, 1) паникует: %v", r)
		}
	}()
	Grow(nil, 1)
}

func TestGrowNoNormalize(t *testing.T) {
	r := Rect{Point{3, 3}, Point{1, 1}}
	Grow(&r, 1)
	if want := (Rect{Point{2, 2}, Point{2, 2}}); r != want {
		t.Errorf("Grow без нормализации: r = %v, want %v", r, want)
	}
}

func TestOverlapEdges(t *testing.T) {
	tests := []struct {
		name   string
		a, b   Rect
		want   Rect
		wantOK bool
	}{
		{"touch side", Rect{Point{0, 0}, Point{2, 2}}, Rect{Point{2, 0}, Point{4, 2}}, Rect{}, false},
		{"touch corner", Rect{Point{0, 0}, Point{2, 2}}, Rect{Point{2, 2}, Point{4, 4}}, Rect{}, false},
		{"inside", Rect{Point{0, 0}, Point{10, 10}}, Rect{Point{2, 3}, Point{4, 5}}, Rect{Point{2, 3}, Point{4, 5}}, true},
		{"unnormalized", Rect{Point{4, 4}, Point{0, 0}}, Rect{Point{6, 6}, Point{2, 2}}, Rect{Point{2, 2}, Point{4, 4}}, true},
		{"one axis only", Rect{Point{0, 0}, Point{4, 4}}, Rect{Point{1, 5}, Point{3, 8}}, Rect{}, false},
		{"empty rect", Rect{Point{1, 1}, Point{1, 5}}, Rect{Point{0, 0}, Point{4, 4}}, Rect{}, false},
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
