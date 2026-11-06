// Строки в Go не имеют методов — вся работа с ними
// в пакете `strings`. Числа ↔ строки — в `strconv`.

package main

import (
	"fmt"
	"strconv"
	"strings"
)

func main() {

	// Поиск и проверки. `EqualFold` сравнивает без учёта
	// регистра с учётом Unicode — как `OrdinalIgnoreCase`.
	s := "  Go is fun, Go is fast  "
	fmt.Println(strings.Contains(s, "fun"))
	fmt.Println(strings.Count(s, "Go"), strings.Index(s, "is"))
	fmt.Println(strings.EqualFold("ПРИВЕТ", "привет"))

	// `TrimSpace` — аналог `Trim()` из C#. `HasPrefix` и
	// `HasSuffix` — `StartsWith` и `EndsWith`.
	t := strings.TrimSpace(s)
	fmt.Printf("%q\n", t)
	fmt.Println(strings.HasPrefix(t, "Go"), strings.HasSuffix(t, "fast"))

	// `Replace` с последним аргументом `n` заменяет первые
	// `n` вхождений, `ReplaceAll` — все.
	fmt.Println(strings.Replace(t, "Go", "Rust", 1))
	fmt.Println(strings.ToUpper(strings.ReplaceAll(t, "Go", "go")))

	// `Split` режет по разделителю и сохраняет пустые поля.
	// `Fields` режет по любым пробелам и пустых полей не даёт.
	fmt.Printf("%q\n", strings.Split("a,,b", ","))
	fmt.Printf("%q\n", strings.Fields("  a \t b\n c "))

	// `Cut` делит строку по первому вхождению разделителя
	// и сообщает, нашёлся ли он. Удобно для «ключ=значение».
	key, value, ok := strings.Cut("port=8080", "=")
	fmt.Println(key, value, ok)
	_, _, ok = strings.Cut("port", "=")
	fmt.Println(ok)

	// `strings.Builder` дописывает в растущий буфер.
	// Нулевое значение готово к работе.
	var b strings.Builder
	for i := range 3 {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "item%d", i)
	}
	fmt.Println(b.String(), b.Len())

	// Если части уже в срезе — проще `Join`.
	fmt.Println(strings.Join([]string{"x", "y", "z"}, "-"))

	// Строки сравнивают операторами: `==`, `<`, `>`.
	// Сравнение побайтовое, без культуры и регистра.
	fmt.Println("apple" < "banana", "Zebra" < "apple")

	// `Itoa` — число в строку. `Atoi` — строку в число,
	// с ошибкой вместо исключения.
	fmt.Println(strconv.Itoa(42) + "!")
	n, err := strconv.Atoi("123")
	fmt.Println(n, err)
	_, err = strconv.Atoi("12a")
	fmt.Println(err)

	// `ParseFloat` и `ParseBool` устроены так же. Второй
	// аргумент `ParseFloat` — разрядность: 64 для `float64`.
	f, _ := strconv.ParseFloat("3.25", 64)
	ok, _ = strconv.ParseBool("true")
	fmt.Println(f*2, ok, strconv.Quote("tab\there"))
}
