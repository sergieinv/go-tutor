package task

import (
	"testing"
	"time"
)

var msk = time.FixedZone("MSK", 3*3600)

func day(y int, m time.Month, d, h int) time.Time {
	return time.Date(y, m, d, h, 0, 0, 0, msk)
}

func TestAddWorkdays(t *testing.T) {
	fri := day(2024, 3, 15, 10) // пятница
	tests := []struct {
		name string
		from time.Time
		n    int
		want time.Time
	}{
		{"пятница +1 → понедельник", fri, 1, day(2024, 3, 18, 10)},
		{"пятница -5 → пятница", fri, -5, day(2024, 3, 8, 10)},
		{"среда +2 → пятница", day(2024, 3, 13, 9), 2, day(2024, 3, 15, 9)},
		{"понедельник -1 → пятница", day(2024, 3, 18, 9), -1, day(2024, 3, 15, 9)},
	}
	for _, tt := range tests {
		got := AddWorkdays(tt.from, tt.n)
		if !got.Equal(tt.want) {
			t.Errorf("%s: AddWorkdays(%v, %d) = %v, want %v", tt.name, tt.from, tt.n, got, tt.want)
		}
	}
}

func TestOverlap(t *testing.T) {
	tests := []struct {
		name           string
		a1, a2, b1, b2 time.Time
		want           time.Duration
	}{
		{"частичное", day(2024, 1, 1, 9), day(2024, 1, 1, 12), day(2024, 1, 1, 11), day(2024, 1, 1, 15), time.Hour},
		{"вложенное", day(2024, 1, 1, 9), day(2024, 1, 1, 18), day(2024, 1, 1, 10), day(2024, 1, 1, 12), 2 * time.Hour},
		{"не пересекаются", day(2024, 1, 1, 9), day(2024, 1, 1, 10), day(2024, 1, 1, 11), day(2024, 1, 1, 12), 0},
	}
	for _, tt := range tests {
		if got := Overlap(tt.a1, tt.a2, tt.b1, tt.b2); got != tt.want {
			t.Errorf("%s: Overlap = %v, want %v", tt.name, got, tt.want)
		}
	}
}
