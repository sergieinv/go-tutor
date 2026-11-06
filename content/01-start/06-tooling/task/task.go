package task

// CountVerbs возвращает, сколько аргументов потребляет
// форматная строка: %% — 0, директива %[флаги][ширина][.точность]буква — 1.
// Для некорректного формата возвращает -1.
func CountVerbs(format string) int {
	// TODO: реализуйте функцию
	return 0
}

// CheckCall проверяет вызов Printf(format, <nargs аргументов>):
// "" — всё в порядке, "bad format" — формат некорректен,
// "format reads N args, but call has M" — число аргументов не совпадает.
func CheckCall(format string, nargs int) string {
	// TODO: реализуйте функцию
	return ""
}
