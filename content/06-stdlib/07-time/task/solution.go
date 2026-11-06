package task

import "time"

// AddWorkdays сдвигает t на n рабочих дней (пн–пт) вперёд или назад.
func AddWorkdays(t time.Time, n int) time.Time {
	step := 1
	if n < 0 {
		step, n = -1, -n
	}
	for n > 0 {
		// AddDate — календарный день: время суток сохранится
		// даже при переходе на летнее время.
		t = t.AddDate(0, 0, step)
		if wd := t.Weekday(); wd != time.Saturday && wd != time.Sunday {
			n--
		}
	}
	return t
}

// Overlap возвращает длительность пересечения [aStart, aEnd) и [bStart, bEnd).
func Overlap(aStart, aEnd, bStart, bEnd time.Time) time.Duration {
	// Позднее из начал и раннее из концов. Before/After
	// сравнивают моменты независимо от пояса.
	start := aStart
	if bStart.After(start) {
		start = bStart
	}
	end := aEnd
	if bEnd.Before(end) {
		end = bEnd
	}
	if !end.After(start) {
		return 0
	}
	return end.Sub(start)
}
