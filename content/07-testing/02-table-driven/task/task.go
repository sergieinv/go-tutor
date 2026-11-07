package task

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
	// TODO: заполните таблицу
	return nil
}

// Check прогоняет таблицу против f и возвращает сообщения о расхождениях.
func Check(f func(int) bool, cases []Case) []string {
	// TODO: реализуйте функцию
	return nil
}
