package task

import "fmt"

// IsLeap сообщает, високосный ли год по григорианскому календарю:
// делится на 4, но не на 100, либо делится на 400.
func IsLeap(year int) bool {
	return year%4 == 0 && year%100 != 0 || year%400 == 0
}

// Case — строка таблицы для IsLeap.
type Case struct {
	Name string
	Year int
	Want bool
}

// LeapCases возвращает таблицу случаев для IsLeap.
func LeapCases() []Case {
	return []Case{
		{"не делится на 4", 2023, false},
		{"делится на 4", 2024, true},
		{"делится на 100, но не на 400", 1900, false},
		{"делится на 400", 2000, true},
		{"первый год", 1, false},
	}
}

// Check прогоняет таблицу против f и возвращает сообщения о расхождениях.
func Check(f func(int) bool, cases []Case) []string {
	var msgs []string
	for _, c := range cases {
		if got := f(c.Year); got != c.Want {
			msgs = append(msgs, fmt.Sprintf("%s: IsLeap(%d) = %t, want %t", c.Name, c.Year, got, c.Want))
		}
	}
	return msgs
}
