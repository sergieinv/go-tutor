package task

import (
	"slices"
	"testing"
)

func TestFirstUniqueUnicode(t *testing.T) {
	tests := []struct {
		name  string
		s     string
		wantR rune
		wantI int
	}{
		// "м" и "а" повторяются, первая уникальная — "р" по байтовому индексу 8.
		{"cyrillic", "мамарада", 'р', 8},
		{"after multibyte", "ббв", 'в', 4},
		{"emoji", "🙂🙂x", 'x', 8},
		{"first is unique", "go", 'g', 0},
		{"empty", "", 0, -1},
		{"all repeat", "абаб", 0, -1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, i := FirstUnique(tt.s)
			if r != tt.wantR || i != tt.wantI {
				t.Errorf("FirstUnique(%q) = %q, %d, want %q, %d", tt.s, r, i, tt.wantR, tt.wantI)
			}
		})
	}
}

func TestApplyDiscountEdges(t *testing.T) {
	items := []Item{{"a", 999}, {"b", 1}, {"c", 0}}
	ApplyDiscount(items, 10)
	if want := []Item{{"a", 899}, {"b", 0}, {"c", 0}}; !slices.Equal(items, want) {
		t.Errorf("ApplyDiscount(10) = %v, want %v", items, want)
	}

	free := []Item{{"x", 500}}
	ApplyDiscount(free, 100)
	if free[0].Price != 0 {
		t.Errorf("ApplyDiscount(100): цена %d, want 0", free[0].Price)
	}

	same := []Item{{"y", 700}}
	ApplyDiscount(same, 0)
	if same[0].Price != 700 {
		t.Errorf("ApplyDiscount(0): цена %d, want 700", same[0].Price)
	}

	ApplyDiscount(nil, 50) // не должно паниковать
}

func TestApplyDiscountSubslice(t *testing.T) {
	items := []Item{{"a", 100}, {"b", 100}, {"c", 100}}
	ApplyDiscount(items[1:], 50)
	if want := []Item{{"a", 100}, {"b", 50}, {"c", 50}}; !slices.Equal(items, want) {
		t.Errorf("ApplyDiscount(items[1:], 50) = %v, want %v", items, want)
	}
}
