// Когда вызывающему нужны детали — какое поле, какая
// строка, какой HTTP-код, — sentinel не подходит.
// Тогда объявляют свой тип ошибки: обычную структуру
// с полями и методом `Error() string`.

package main

import (
	"errors"
	"fmt"
)

// Любой тип с методом `Error() string` реализует `error`.
// Реализация неявная: никакого `: Exception` писать
// не нужно. Обычно метод вешают на указатель, а ошибку
// возвращают как `*ValidationError`.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("поле %s: %s", e.Field, e.Reason)
}

// Сигнатура возвращает `error`, а не `*ValidationError`:
// вызывающему не нужно знать конкретный тип, пока он
// не захочет посмотреть детали.
func validateName(name string) error {
	if name == "" {
		return &ValidationError{Field: "name", Reason: "пустое значение"}
	}
	if len(name) > 5 {
		return &ValidationError{Field: "name", Reason: "длиннее 5 байт"}
	}
	return nil
}

func main() {

	// Чтобы достать поля, ошибку приводят к своему типу.
	// `errors.As` находит в ошибке значение нужного типа
	// и записывает его в переменную по указателю. Как и
	// `errors.Is`, он смотрит всю цепочку обёрток.
	for _, name := range []string{"bob", "", "alexander"} {
		err := validateName(name)
		var ve *ValidationError
		if errors.As(err, &ve) {
			fmt.Printf("%q: ошибка в поле %s (%s)\n", name, ve.Field, ve.Reason)
			continue
		}
		fmt.Printf("%q: ок\n", name)
	}

	// Ошибка — интерфейс, поэтому подходит и type switch.
	// Но он не видит обёрток: для обёрнутой ошибки
	// сработает только `errors.As`.
	err := validateName("")
	switch e := err.(type) {
	case nil:
		fmt.Println("нет ошибки")
	case *ValidationError:
		fmt.Println("type switch:", e.Field)
	default:
		fmt.Println("другая ошибка:", e)
	}

	// Главная ловушка: интерфейс равен `nil`, только если
	// в нём нет ни типа, ни значения. `nil`-указатель
	// типа `*ValidationError`, положенный в `error`, даёт
	// интерфейс с типом — и он уже не `nil`.
	var ve *ValidationError
	var e error = ve
	fmt.Println("ve == nil:", ve == nil)
	fmt.Println("e == nil:", e == nil)
	fmt.Printf("внутри e: %T\n", e)
}
