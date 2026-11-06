package task

import (
	"fmt"
	"time"
)

// Раскладки без смещения — по порядку попыток.
var localLayouts = []string{"2006-01-02 15:04:05", "2006-01-02", "02.01.2006"}

// ParseFlexible разбирает время в одном из форматов: RFC3339,
// "2006-01-02 15:04:05", "2006-01-02", "02.01.2006".
// Время без смещения трактуется в поясе loc (nil — UTC).
func ParseFlexible(s string, loc *time.Location) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t, nil
	}
	if loc == nil {
		loc = time.UTC // ParseInLocation с nil паникует
	}
	for _, layout := range localLayouts {
		if t, err := time.ParseInLocation(layout, s, loc); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("неизвестный формат времени: %q", s)
}

// FormatHuman печатает время как "05.03.2024 15:04 +03:00".
func FormatHuman(t time.Time) string {
	return t.Format("02.01.2006 15:04 -07:00")
}
