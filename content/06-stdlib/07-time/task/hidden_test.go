package task

import (
	"testing"
	"time"
)

func TestAddWorkdaysWeekendStart(t *testing.T) {
	sat := day(2024, 3, 16, 8)
	if got := AddWorkdays(sat, 0); got != sat {
		t.Errorf("AddWorkdays(сб, 0) = %v, want %v без изменений", got, sat)
	}
	if got, want := AddWorkdays(sat, 1), day(2024, 3, 18, 8); !got.Equal(want) {
		t.Errorf("AddWorkdays(сб, 1) = %v, want %v", got, want)
	}
	if got, want := AddWorkdays(sat, -1), day(2024, 3, 15, 8); !got.Equal(want) {
		t.Errorf("AddWorkdays(сб, -1) = %v, want %v", got, want)
	}
	sun := day(2024, 3, 17, 8)
	if got, want := AddWorkdays(sun, 5), day(2024, 3, 22, 8); !got.Equal(want) {
		t.Errorf("AddWorkdays(вс, 5) = %v, want %v", got, want)
	}
}

func TestAddWorkdaysLongAndLocation(t *testing.T) {
	from := day(2024, 2, 28, 23) // среда, 23:00 MSK — в UTC это ещё 20:00 среды
	got := AddWorkdays(from, 10)
	want := day(2024, 3, 13, 23)
	if !got.Equal(want) || got.Location() != msk {
		t.Errorf("AddWorkdays(%v, 10) = %v, want %v в поясе MSK", from, got, want)
	}
	// Пятница 01:00 в поясе +03:00 — в UTC это ещё четверг.
	fri := time.Date(2024, 3, 15, 1, 30, 0, 0, msk)
	if got, want := AddWorkdays(fri, 1), time.Date(2024, 3, 18, 1, 30, 0, 0, msk); !got.Equal(want) {
		t.Errorf("AddWorkdays(пт 01:30 MSK, 1) = %v, want %v: день недели — в поясе t", got, want)
	}
	if got, want := AddWorkdays(day(2024, 3, 22, 7), -20), day(2024, 2, 23, 7); !got.Equal(want) {
		t.Errorf("AddWorkdays(-20) = %v, want %v", got, want)
	}
}

func TestAddWorkdaysDST(t *testing.T) {
	// Перевод часов: в зоне с летним временем сутки бывают по 23 часа.
	loc, err := time.LoadLocation("Europe/Berlin")
	if err != nil {
		t.Skip("нет базы часовых поясов")
	}
	fri := time.Date(2024, 3, 29, 9, 0, 0, 0, loc) // в ночь на 31 марта — переход
	got := AddWorkdays(fri, 1)
	if got.Hour() != 9 || got.Day() != 1 || got.Month() != time.April {
		t.Errorf("AddWorkdays через перевод часов = %v, want 2024-04-01 09:00", got)
	}
}

func TestOverlapEdges(t *testing.T) {
	utc := func(h int) time.Time { return time.Date(2024, 1, 1, h, 0, 0, 0, time.UTC) }
	tests := []struct {
		name           string
		a1, a2, b1, b2 time.Time
		want           time.Duration
	}{
		{"касаются", utc(9), utc(10), utc(10), utc(11), 0},
		{"перевёрнутый", utc(12), utc(9), utc(8), utc(13), 0},
		{"пустой", utc(10), utc(10), utc(8), utc(13), 0},
		{"совпадают", utc(8), utc(13), utc(8), utc(13), 5 * time.Hour},
		{"b раньше a", utc(11), utc(15), utc(9), utc(12), time.Hour},
		// 12:00 MSK == 09:00 UTC
		{"разные пояса", utc(9), utc(12), day(2024, 1, 1, 13), day(2024, 1, 1, 20), 2 * time.Hour},
		{"разные пояса, не пересекаются", utc(6), utc(9), day(2024, 1, 1, 12), day(2024, 1, 1, 14), 0},
	}
	for _, tt := range tests {
		if got := Overlap(tt.a1, tt.a2, tt.b1, tt.b2); got != tt.want {
			t.Errorf("%s: Overlap = %v, want %v", tt.name, got, tt.want)
		}
	}
}
