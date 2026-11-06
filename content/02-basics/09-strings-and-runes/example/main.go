// Строка в Go — неизменяемые байты, обычно UTF-8.
// Символ Unicode называется руной: `rune` — это
// `int32` с кодовой точкой.

package main

import (
	"fmt"
	"unicode/utf8"
)

func main() {

	// `len` возвращает число байтов, а не символов.
	// Латиница занимает 1 байт, кириллица — 2.
	s := "Go, мир"
	fmt.Println("bytes:", len(s))
	fmt.Println("runes:", utf8.RuneCountInString(s))

	// Индекс даёт `byte` (`uint8`). `s[4]` — первый
	// из двух байтов буквы «м», а не сама буква.
	fmt.Println("s[0]:", s[0], "s[4]:", s[4])
	fmt.Printf("hex: % x\n", s)

	// `range` декодирует UTF-8: `i` — смещение в байтах,
	// `r` — руна. Смещения идут с шагом 1 или 2.
	for i, r := range s {
		fmt.Printf("%d:%c(%d) ", i, r, utf8.RuneLen(r))
	}
	fmt.Println()

	// Литерал руны — в одинарных кавычках. `%U` печатает
	// кодовую точку, `%c` — сам символ.
	r := 'ж'
	fmt.Printf("rune: %c %U %d\n", r, r, r)

	// Срез строки — тоже по байтам и без копирования.
	// `s[4:6]` — ровно одна буква «м».
	fmt.Println("slice:", s[1:4], s[4:6])

	// `[]rune(s)` декодирует строку в срез кодовых точек:
	// теперь индекс — это номер символа.
	runes := []rune(s)
	fmt.Println("runes[4]:", string(runes[4]), len(runes))

	// `[]byte(s)` копирует байты: строку изменить нельзя,
	// а копию — можно. `string(b)` копирует обратно.
	b := []byte(s)
	b[0] = 'g'
	fmt.Println("copy:", string(b), s)

	// Конкатенация создаёт новую строку. Исходная
	// не меняется — её байты неизменяемы.
	u := s + "!"
	fmt.Println("concat:", u, len(u))

	// Строки сравниваются побайтово операторами `==` и `<`.
	fmt.Println("compare:", "abc" < "abd", "мир" == s[4:])

	// Строка может содержать и некорректный UTF-8.
	// `range` превращает такой байт в руну U+FFFD.
	bad := s[:5]
	fmt.Println("valid:", utf8.ValidString(bad))
	for _, r := range bad[4:] {
		fmt.Printf("bad: %U\n", r)
	}
}
