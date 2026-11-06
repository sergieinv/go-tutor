// `switch` в Go выполняет одну подходящую ветку
// и выходит: `break` писать не нужно.

package main

import (
	"fmt"
	"path"
)

func main() {

	// Несколько значений в одном `case` — через запятую.
	// `default` срабатывает, если не подошло ничего.
	for _, day := range []string{"sat", "tue", "sun"} {
		switch day {
		case "sat", "sun":
			fmt.Println(day, "— выходной")
		default:
			fmt.Println(day, "— рабочий")
		}
	}

	// Как и у `if`, у `switch` бывает init-выражение.
	// `ext` видна только внутри этого `switch`.
	switch ext := path.Ext("report.pdf"); ext {
	case ".txt", ".md":
		fmt.Println("текст")
	case ".pdf":
		fmt.Println("документ")
	}

	// `switch` без выражения — замена цепочки
	// `if / else if`. Ветки проверяются сверху вниз,
	// выполняется первая, где условие истинно.
	for _, t := range []int{-5, 12, 30} {
		switch {
		case t < 0:
			fmt.Println(t, "мороз")
		case t < 20:
			fmt.Println(t, "прохладно")
		default:
			fmt.Println(t, "тепло")
		}
	}

	// Значения в `case` не обязаны быть константами:
	// подойдут переменные и вызовы функций.
	target := 9
	switch target {
	case square(2):
		fmt.Println("четыре")
	case square(3):
		fmt.Println("девять")
	}

	// `fallthrough` явно передаёт управление в тело
	// следующей ветки — без проверки её условия.
	// Нужен редко: обычно хватает `case a, b:`.
	level := 2
	switch level {
	case 2:
		fmt.Print("подробно → ")
		fallthrough
	case 1:
		fmt.Print("кратко → ")
		fallthrough
	default:
		fmt.Println("заголовок")
	}
}

func square(n int) int { return n * n }
