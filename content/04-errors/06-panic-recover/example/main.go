// `panic` — аварийная остановка: обычное выполнение
// прекращается, стек раскручивается, отложенные
// вызовы выполняются. Если никто не вызвал `recover`,
// программа падает. Это для багов, а не для «файл
// не найден» — ожидаемые сбои возвращают как `error`.

package main

import (
	"errors"
	"fmt"
	"runtime"
)

// Паника уместна, когда нарушен инвариант и продолжать
// бессмысленно: вызывающий передал невозможные данные.
// Так делают `Must`-функции и рантайм при выходе
// за границы среза или делении на ноль.
func mustPositive(n int) int {
	if n <= 0 {
		panic(fmt.Sprintf("mustPositive: %d <= 0", n))
	}
	return n
}

// `recover` останавливает раскрутку, но только если
// вызван прямо в отложенной функции. На границе
// компонента панику превращают в ошибку и записывают
// в именованный результат — так делает `net/http`
// для каждого обработчика запроса.
func safely(name string, f func()) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s: panic: %v", name, r)
		}
	}()
	f()
	return nil
}

func main() {
	fmt.Println(safely("ok", func() { mustPositive(5) }))
	fmt.Println(safely("bad arg", func() { mustPositive(-1) }))

	// Рантайм паникует значением, реализующим
	// `runtime.Error`. Его можно отличить от паник
	// со строкой через `errors.As`.
	err := safely("index", func() {
		s, i := []int{1, 2, 3}, 5
		fmt.Println(s[i])
	})
	fmt.Println(err)

	// Через `%v` тип паники остался только в тексте.
	// Чтобы проверить тип, сохраним само значение.
	var r any
	func() {
		defer func() { r = recover() }()
		var m map[string]int
		m["x"] = 1 // запись в nil-map — тоже паника рантайма
	}()
	var re runtime.Error
	fmt.Println("runtime.Error:", errors.As(r.(error), &re), "—", r)

	// Re-panic: обработчик разбирает только знакомые
	// паники, а остальные пробрасывает дальше, вызвав
	// `panic` повторно. Здесь её ловит внешний `safely`.
	err = safely("outer", func() {
		defer func() {
			r := recover()
			if s, ok := r.(string); ok && s == "retry" {
				fmt.Println("обработали retry")
				return
			}
			panic(r) // чужая паника — не наше дело
		}()
		panic("disk on fire")
	})
	fmt.Println(err)
}
