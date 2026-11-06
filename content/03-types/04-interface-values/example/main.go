// Интерфейсное значение — пара (динамический тип, значение).
// Type assertion и type switch проверяют тип и достают
// значение, а сравнение с `nil` смотрит на оба слова.

package main

import (
	"fmt"
	"io"
	"strings"
)

type NotFound struct{ Key string }

func (e *NotFound) Error() string { return e.Key + " not found" }

// Ошибка: переменная конкретного типа возвращается как
// интерфейс. Даже если указатель `nil`, тип в паре есть.
func findBad(key string) error {
	var err *NotFound
	if key == "" {
		err = &NotFound{key}
	}
	return err
}

// Правильно: на успешном пути возвращаем `nil` явно.
func findGood(key string) error {
	if key == "" {
		return &NotFound{key}
	}
	return nil
}

func describe(v any) string {
	// Type switch: в каждой ветке `x` имеет тип этой
	// ветки. Ветки проверяются сверху вниз.
	switch x := v.(type) {
	case nil:
		return "nil-интерфейс"
	case int:
		return fmt.Sprintf("int, x+1 = %d", x+1)
	case string, []byte:
		// В ветке с несколькими типами `x` остаётся `any`.
		return fmt.Sprintf("текст %T", x)
	case error:
		return "ошибка: " + x.Error()
	default:
		return fmt.Sprintf("что-то ещё: %T", x)
	}
}

func main() {
	// `%T` печатает динамический тип из пары.
	var r io.Reader = strings.NewReader("abc")
	fmt.Printf("%T\n", r)

	// Assertion к конкретному типу. Форма с `ok`
	// не паникует при несовпадении.
	sr, ok := r.(*strings.Reader)
	fmt.Println(sr.Len(), ok)

	// Assertion к другому интерфейсу: умеет ли значение
	// ещё и `io.Writer`? `*strings.Reader` — нет.
	_, ok = r.(io.Writer)
	fmt.Println("writer:", ok)

	for _, v := range []any{nil, 41, "go", []byte("x"), &NotFound{"k"}, 3.5} {
		fmt.Println(describe(v))
	}

	// Главная ловушка: в интерфейсе nil-указатель,
	// но тип в паре заполнен — интерфейс не `nil`.
	err := findBad("user")
	fmt.Println(err == nil, fmt.Sprintf("%T", err))
	fmt.Println(findGood("user") == nil)
}
