package task

// Season возвращает время года по номеру месяца 1..12:
// "winter", "spring", "summer", "autumn" или "invalid".
func Season(month int) string {
	switch month {
	case 12, 1, 2:
		return "winter"
	case 3, 4, 5:
		return "spring"
	case 6, 7, 8:
		return "summer"
	case 9, 10, 11:
		return "autumn"
	default:
		return "invalid"
	}
}

// StatusClass возвращает класс HTTP-кода: "informational", "success",
// "redirect", "client error", "rate limited" (429), "server error"
// или "unknown".
func StatusClass(code int) string {
	// Частный случай ставим раньше общего диапазона:
	// выполняется первая подходящая ветка.
	switch {
	case code == 429:
		return "rate limited"
	case code >= 100 && code < 200:
		return "informational"
	case code >= 200 && code < 300:
		return "success"
	case code >= 300 && code < 400:
		return "redirect"
	case code >= 400 && code < 500:
		return "client error"
	case code >= 500 && code < 600:
		return "server error"
	default:
		return "unknown"
	}
}
