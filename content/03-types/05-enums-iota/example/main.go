// Перечисление в Go — именованный тип плюс блок
// констант. `iota` нумерует строки блока с нуля.

package main

import (
	"fmt"
	"strings"
)

type Weekday int

// Выражение `= iota` указано только у первой константы.
// Остальные повторяют его, а `iota` растёт: 0, 1, 2…
const (
	Sunday Weekday = iota
	Monday
	Tuesday
	Wednesday
)

var weekdayNames = [...]string{"Sunday", "Monday", "Tuesday", "Wednesday"}

// `String()` делает значение читаемым: `fmt` вызывает его
// для `%v` и `Println`. Значение вне диапазона — тоже
// допустимое число, обрабатываем его отдельно.
func (d Weekday) String() string {
	if d < 0 || int(d) >= len(weekdayNames) {
		return fmt.Sprintf("Weekday(%d)", int(d))
	}
	return weekdayNames[d]
}

// `_` пропускает значение: здесь — нулевую степень.
// В формуле можно использовать `iota` как число.
type ByteSize int64

const (
	_           = iota
	KB ByteSize = 1 << (10 * iota)
	MB
	GB
)

// Битовые флаги: каждая константа — отдельный бит.
type Style uint8

const (
	Bold Style = 1 << iota
	Italic
	Underline
)

func (s Style) String() string {
	var parts []string
	for i, name := range []string{"bold", "italic", "underline"} {
		if s&(1<<i) != 0 {
			parts = append(parts, name)
		}
	}
	return "[" + strings.Join(parts, "|") + "]"
}

func main() {
	fmt.Println(Monday, int(Monday))
	fmt.Printf("%v %d\n", Wednesday, Wednesday)

	// Нулевое значение типа — первая константа.
	var d Weekday
	fmt.Println(d)

	// Диапазон не проверяется: 42 — тоже `Weekday`.
	fmt.Println(Weekday(42))

	fmt.Println(KB, MB, GB)

	// Флаги объединяют через `|`, проверяют через `&`,
	// снимают через `&^` (AND NOT).
	s := Bold | Underline
	fmt.Println(s, s&Italic != 0, s&Underline != 0)
	s &^= Bold
	fmt.Println(s, uint8(s))
}
