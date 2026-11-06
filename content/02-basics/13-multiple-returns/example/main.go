// Функция может вернуть несколько значений. На этом
// держатся идиомы `(value, error)` и `(value, ok)`.

package main

import (
	"errors"
	"fmt"
	"strconv"
)

// Типы результатов перечисляют в скобках.
func divmod(a, b int) (int, int) {
	return a / b, a % b
}

// Идиома `(value, error)`: ошибка — последний результат,
// `nil` означает успех. При ошибке остальные результаты —
// нулевые значения.
func safeDiv(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("деление на ноль")
	}
	return a / b, nil
}

// Именованные результаты — переменные, которые уже
// объявлены и равны нулевым значениям. Голый `return`
// возвращает их текущие значения. Годится для коротких
// функций, где имена документируют смысл результатов.
func split(sum int) (x, y int) {
	x = sum * 4 / 9
	y = sum - x
	return
}

func main() {
	q, r := divmod(17, 5)
	fmt.Println("divmod:", q, r)

	// Ненужный результат отбрасывают через `_`.
	_, rem := divmod(10, 3)
	fmt.Println("rem:", rem)

	// Проверка ошибки сразу после вызова — главный
	// шаблон Go. Исключений нет, ошибка — обычное значение.
	if v, err := safeDiv(1, 0); err != nil {
		fmt.Println("error:", err)
	} else {
		fmt.Println("value:", v)
	}

	// Стандартная библиотека устроена так же.
	n, err := strconv.Atoi("42")
	fmt.Println("atoi:", n, err)

	// Идиома «запятая-ok»: второй результат сообщает,
	// есть ли значение. Аналог `TryGetValue`.
	ages := map[string]int{"ann": 30}
	if age, ok := ages["bob"]; !ok {
		fmt.Println("bob: нет, age =", age)
	}

	x, y := split(17)
	fmt.Println("split:", x, y)

	// Все результаты одной функции можно передать
	// аргументами в другую — если больше аргументов нет.
	fmt.Println(divmod(divmod(100, 7)))

	// Обменять значения можно без временной переменной:
	// правая часть вычисляется целиком до присваивания.
	a, b := "left", "right"
	a, b = b, a
	fmt.Println("swap:", a, b)
}
