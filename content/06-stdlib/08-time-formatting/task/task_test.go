package task

import (
	"testing"
	"time"
)

var msk = time.FixedZone("MSK", 3*3600)

func TestParseFlexible(t *testing.T) {
	tests := []struct {
		in   string
		want time.Time
	}{
		{"2024-03-05T10:00:00+03:00", time.Date(2024, 3, 5, 7, 0, 0, 0, time.UTC)},
		{"2024-03-05 10:20:30", time.Date(2024, 3, 5, 10, 20, 30, 0, msk)},
		{"2024-03-05", time.Date(2024, 3, 5, 0, 0, 0, 0, msk)},
		{"05.03.2024", time.Date(2024, 3, 5, 0, 0, 0, 0, msk)},
	}
	for _, tt := range tests {
		got, err := ParseFlexible(tt.in, msk)
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("ParseFlexible(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
}

func TestParseFlexibleError(t *testing.T) {
	if got, err := ParseFlexible("5 марта", msk); err == nil {
		t.Errorf("ParseFlexible(\"5 марта\") = %v, nil; want ошибку", got)
	}
}

func TestFormatHuman(t *testing.T) {
	tm := time.Date(2024, 3, 5, 15, 4, 9, 0, msk)
	if got, want := FormatHuman(tm), "05.03.2024 15:04 +03:00"; got != want {
		t.Errorf("FormatHuman = %q, want %q", got, want)
	}
}
