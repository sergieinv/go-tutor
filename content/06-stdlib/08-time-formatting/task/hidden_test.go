package task

import (
	"testing"
	"time"
)

func TestParseFlexibleLocation(t *testing.T) {
	for _, in := range []string{"2024-03-05 10:20:30", "2024-03-05", "05.03.2024"} {
		got, err := ParseFlexible(in, msk)
		if err != nil || got.Location() != msk {
			t.Errorf("ParseFlexible(%q, msk): пояс %v, ошибка %v; want MSK", in, got.Location(), err)
		}
		got, err = ParseFlexible(in, nil)
		if err != nil || got.Location() != time.UTC {
			t.Errorf("ParseFlexible(%q, nil): пояс %v, ошибка %v; want UTC", in, got.Location(), err)
		}
	}
	got, err := ParseFlexible("2024-03-05 10:00:00", nil)
	if want := time.Date(2024, 3, 5, 10, 0, 0, 0, time.UTC); err != nil || !got.Equal(want) {
		t.Errorf("ParseFlexible с nil = %v, %v; want %v", got, err, want)
	}
}

func TestParseFlexibleRFC3339IgnoresLoc(t *testing.T) {
	got, err := ParseFlexible("2024-12-31T23:59:59Z", msk)
	want := time.Date(2024, 12, 31, 23, 59, 59, 0, time.UTC)
	if err != nil || !got.Equal(want) {
		t.Errorf("ParseFlexible(Z) = %v, %v; want %v", got, err, want)
	}
	got, err = ParseFlexible("2024-03-05T10:00:00.5-05:00", nil)
	want = time.Date(2024, 3, 5, 15, 0, 0, 500_000_000, time.UTC)
	if err != nil || !got.Equal(want) {
		t.Errorf("ParseFlexible(дробные секунды) = %v, %v; want %v", got, err, want)
	}
}

func TestParseFlexibleErrors(t *testing.T) {
	bad := []string{
		"", " 2024-03-05", "2024-03-05 ", "2024-02-30", "31.02.2024",
		"2024-03-05 10:20", "2024-3-5", "03/05/2024", "2024-03-05T10:00:00",
		"1709640249", "05.03.24",
	}
	for _, in := range bad {
		got, err := ParseFlexible(in, msk)
		if err == nil {
			t.Errorf("ParseFlexible(%q) = %v, nil; want ошибку", in, got)
		} else if !got.IsZero() {
			t.Errorf("ParseFlexible(%q) при ошибке вернул %v, want нулевое время", in, got)
		}
	}
}

func TestFormatHumanZones(t *testing.T) {
	tests := []struct {
		in   time.Time
		want string
	}{
		{time.Date(2024, 1, 9, 7, 5, 0, 0, time.UTC), "09.01.2024 07:05 +00:00"},
		{time.Date(2024, 12, 31, 23, 59, 59, 999, time.FixedZone("", -(5*3600+30*60))), "31.12.2024 23:59 -05:30"},
		{time.Date(2024, 3, 5, 0, 0, 0, 0, time.UTC).In(msk), "05.03.2024 03:00 +03:00"},
	}
	for _, tt := range tests {
		if got := FormatHuman(tt.in); got != tt.want {
			t.Errorf("FormatHuman(%v) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
