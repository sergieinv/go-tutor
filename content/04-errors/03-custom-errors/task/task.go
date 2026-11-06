package task

// BracketError описывает первую ошибку в расстановке скобок.
type BracketError struct {
	Pos  int  // байтовый индекс ошибочного символа или len(s), если строка кончилась
	Char rune // ошибочный символ; 0, если строка кончилась
}

// Error возвращает "unexpected ']' at 3" или "unexpected end at 5".
func (e *BracketError) Error() string {
	// TODO: реализуйте метод
	return ""
}

// Check проверяет расстановку скобок (), [], {} в s.
// Возвращает nil или *BracketError с первой ошибкой.
func Check(s string) error {
	// TODO: реализуйте функцию
	return nil
}
