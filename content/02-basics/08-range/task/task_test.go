package task

import (
	"slices"
	"testing"
)

func TestFirstUnique(t *testing.T) {
	tests := []struct {
		s     string
		wantR rune
		wantI int
	}{
		{"swiss", 'w', 1},
		{"aabb", 0, -1},
	}
	for _, tt := range tests {
		r, i := FirstUnique(tt.s)
		if r != tt.wantR || i != tt.wantI {
			t.Errorf("FirstUnique(%q) = %q, %d, want %q, %d", tt.s, r, i, tt.wantR, tt.wantI)
		}
	}
}

func TestApplyDiscount(t *testing.T) {
	items := []Item{{"pen", 1000}, {"cup", 250}}
	ApplyDiscount(items, 20)
	want := []Item{{"pen", 800}, {"cup", 200}}
	if !slices.Equal(items, want) {
		t.Errorf("после ApplyDiscount(20) = %v, want %v", items, want)
	}
}
