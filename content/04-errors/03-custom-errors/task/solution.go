package task

import "fmt"

// BracketError описывает первую ошибку в расстановке скобок.
type BracketError struct {
	Pos  int  // байтовый индекс ошибочного символа или len(s), если строка кончилась
	Char rune // ошибочный символ; 0, если строка кончилась
}

// Error возвращает "unexpected ']' at 3" или "unexpected end at 5".
func (e *BracketError) Error() string {
	if e.Char == 0 {
		return fmt.Sprintf("unexpected end at %d", e.Pos)
	}
	return fmt.Sprintf("unexpected %q at %d", e.Char, e.Pos)
}

var pairs = map[rune]rune{')': '(', ']': '[', '}': '{'}

// Check проверяет расстановку скобок (), [], {} в s.
// Возвращает nil или *BracketError с первой ошибкой.
func Check(s string) error {
	var stack []rune
	for i, c := range s {
		switch c {
		case '(', '[', '{':
			stack = append(stack, c)
		case ')', ']', '}':
			if len(stack) == 0 || stack[len(stack)-1] != pairs[c] {
				return &BracketError{Pos: i, Char: c}
			}
			stack = stack[:len(stack)-1]
		}
	}
	if len(stack) > 0 {
		return &BracketError{Pos: len(s)}
	}
	// Литерал nil, а не переменная *BracketError: иначе
	// вызывающий получил бы не-nil интерфейс.
	return nil
}
