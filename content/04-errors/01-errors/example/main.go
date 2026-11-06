// В Go ошибка — обычное значение типа `error`. Функция,
// которая может не справиться, возвращает её последним
// результатом, а вызывающий код сразу проверяет её
// на `nil`. Исключений для ожидаемых сбоев нет.

package main

import (
	"errors"
	"fmt"
	"strconv"
)

// `error` — встроенный интерфейс с одним методом
// `Error() string`. `errors.New` создаёт простейшую
// ошибку с фиксированным текстом.
func checkAge(age int) error {
	if age < 0 {
		return errors.New("возраст не может быть отрицательным")
	}
	return nil // nil — «ошибки нет»
}

// Ошибка — последний результат. При ошибке остальные
// результаты — нулевые значения, вызывающий код их
// не читает. `fmt.Errorf` форматирует текст ошибки
// и добавляет контекст: что именно мы делали.
func parseAge(s string) (int, error) {
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0, fmt.Errorf("разбор возраста %q: %v", s, err)
	}
	if err := checkAge(n); err != nil {
		return 0, err
	}
	return n, nil
}

func main() {

	// Главная идиома Go: вызвали — сразу проверили.
	// Успешный путь идёт по левому краю, обработка
	// ошибок — в коротких `if`.
	for _, s := range []string{"42", "abc", "-5"} {
		age, err := parseAge(s)
		if err != nil {
			fmt.Println("ошибка:", err)
			continue
		}
		fmt.Println("возраст:", age)
	}

	// Ошибка — значение: её можно сохранить в переменную,
	// положить в срез, сравнить с `nil` и напечатать.
	// `Println` вызывает у неё метод `Error()`.
	var errs []error
	for _, s := range []string{"1", "x", "2", "y"} {
		if _, err := strconv.Atoi(s); err != nil {
			errs = append(errs, err)
		}
	}
	fmt.Println("ошибок:", len(errs))
	fmt.Println("первая:", errs[0].Error())

	// Чтобы проигнорировать ошибку, её нужно выбросить
	// явно через `_`. Так видно, что решение принято
	// сознательно, — но результат при этом нулевой.
	n, _ := strconv.Atoi("12a")
	fmt.Println("без проверки:", n)
}
