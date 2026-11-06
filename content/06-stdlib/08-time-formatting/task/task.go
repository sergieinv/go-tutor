package task

import "time"

// ParseFlexible разбирает время в одном из форматов: RFC3339,
// "2006-01-02 15:04:05", "2006-01-02", "02.01.2006".
// Время без смещения трактуется в поясе loc (nil — UTC).
func ParseFlexible(s string, loc *time.Location) (time.Time, error) {
	// TODO: реализуйте функцию
	return time.Time{}, nil
}

// FormatHuman печатает время как "05.03.2024 15:04 +03:00".
func FormatHuman(t time.Time) string {
	// TODO: реализуйте функцию
	return ""
}
