package task

// Reverse переворачивает строку по рунам.
func Reverse(s string) string {
	// []rune декодирует UTF-8: переставляем символы, а не байты.
	r := []rune(s)
	for i, j := 0, len(r)-1; i < j; i, j = i+1, j-1 {
		r[i], r[j] = r[j], r[i]
	}
	return string(r)
}

// Truncate оставляет первые n рун строки s.
// Если рун не больше n, s возвращается как есть; при n <= 0 — "".
func Truncate(s string, n int) string {
	if n <= 0 {
		return ""
	}
	count := 0
	// range отдаёт байтовые смещения начала рун —
	// по ним можно резать, не разрывая символ.
	for i := range s {
		if count == n {
			return s[:i]
		}
		count++
	}
	return s
}
