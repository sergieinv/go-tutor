package task

import "strings"

// Banner рисует ASCII-рамку вокруг title:
//
//	+-------+
//	| Hello |
//	+-------+
//
// Строки разделены "\n", в конце перевода строки нет.
func Banner(title string) string {
	// По пробелу с каждой стороны — рамка на 2 шире текста.
	border := "+" + strings.Repeat("-", len(title)+2) + "+"
	return border + "\n| " + title + " |\n" + border
}
