// `strconv.Atoi` — только начало. Полные функции разбора
// принимают систему счисления и размер результата в битах
// и возвращают подробную ошибку `*strconv.NumError`.

package main

import (
	"errors"
	"fmt"
	"strconv"
)

func main() {
	// `ParseInt(s, base, bitSize)`: `base` от 2 до 36,
	// `bitSize` — во что результат должен поместиться
	// (0 — `int`). Результат всегда `int64` — приводите сами.
	n, err := strconv.ParseInt("-ff", 16, 64)
	fmt.Println(n, err)
	b, _ := strconv.ParseInt("1010", 2, 8)
	fmt.Println(int8(b))

	// `base = 0` — система из префикса, как в исходниках Go:
	// `0x`, `0o` (или ведущий `0`), `0b`; разрешены `_`.
	var vals []int64
	for _, s := range []string{"0x1F", "0o17", "017", "0b101", "1_000_000"} {
		v, _ := strconv.ParseInt(s, 0, 64)
		vals = append(vals, v)
	}
	fmt.Println(vals)

	// Не влезло в `bitSize` — ошибка `ErrRange`, а значение —
	// ближайшая граница типа, а не ноль.
	v, err := strconv.ParseInt("300", 10, 8)
	fmt.Println(v, err)

	// Ошибка — `*strconv.NumError` с полями `Func`, `Num`, `Err`.
	// Причину проверяют через `errors.Is` с `ErrRange`
	// или `ErrSyntax`.
	var ne *strconv.NumError
	if errors.As(err, &ne) {
		fmt.Println(ne.Func, ne.Num, errors.Is(err, strconv.ErrRange))
	}
	_, err = strconv.ParseInt("12a", 10, 64)
	fmt.Println(errors.Is(err, strconv.ErrSyntax))

	// `ParseUint` не принимает знак — даже `+`.
	_, err = strconv.ParseUint("+5", 10, 64)
	fmt.Println(err)

	// `ParseFloat` понимает экспоненту, `Inf`, `NaN`
	// и шестнадцатеричную запись. Разделитель — только точка,
	// культур нет.
	for _, s := range []string{"1.5e3", "-Inf", "0x1p-2", "1,5"} {
		f, err := strconv.ParseFloat(s, 64)
		fmt.Println(f, err)
	}

	// `ParseBool`: 1, t, T, TRUE, true, True и то же для false.
	ok, _ := strconv.ParseBool("T")
	_, err = strconv.ParseBool("yes")
	fmt.Println(ok, err)

	// Обратно: `FormatInt` в любой системе от 2 до 36,
	// `FormatFloat` с форматом и точностью,
	// `AppendInt` — без лишней строки.
	fmt.Println(strconv.FormatInt(255, 2), strconv.FormatInt(255, 16),
		strconv.FormatInt(1295, 36), strconv.FormatFloat(3.14159, 'f', 2, 64))
	fmt.Println(string(strconv.AppendInt([]byte("id="), 42, 10)))

	// `Quote` превращает строку в литерал Go с экранированием,
	// `Unquote` — обратно.
	q := strconv.Quote("tab\t\"кавычки\"")
	s, _ := strconv.Unquote(q)
	fmt.Println(q, strconv.QuoteToASCII("я"), s)
}
