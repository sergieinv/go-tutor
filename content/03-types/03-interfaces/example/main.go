// Интерфейс — набор методов. Тип реализует его неявно:
// достаточно иметь методы с нужными сигнатурами.

package main

import (
	"fmt"
	"strings"
)

// Маленький интерфейс из одного метода. Имя по
// соглашению — метод плюс суффикс `-er`.
type Namer interface {
	Name() string
}

// Ни `Dog`, ни `File` не упоминают `Namer`, но оба
// его реализуют — у них есть `Name() string`.
type Dog struct{ nick string }

func (d Dog) Name() string { return "dog " + d.nick }

type File struct{ path string }

func (f *File) Name() string { return f.path[strings.LastIndex(f.path, "/")+1:] }

// Проверка на этапе компиляции: если сигнатуры
// разъедутся, сборка упадёт здесь, а не где-то далеко.
// `(*File)(nil)` — типизированный nil, память не нужна.
var (
	_ Namer = Dog{}
	_ Namer = (*File)(nil)
)

// Функция принимает интерфейс — ей подходит что угодно
// с методом `Name`. Это и есть «принимай интерфейсы».
func greet(n Namer) string {
	return "hello, " + n.Name()
}

// Интерфейс можно объявить там, где он нужен, даже
// для чужого типа: `strings.Builder` о нём не знает.
type stringer interface{ String() string }

func main() {
	fmt.Println(greet(Dog{"rex"}))
	fmt.Println(greet(&File{"/tmp/data.csv"}))

	// Срез интерфейсов хранит значения разных типов.
	all := []Namer{Dog{"max"}, &File{"/etc/hosts"}}
	for _, n := range all {
		fmt.Println(n.Name())
	}

	var b strings.Builder
	b.WriteString("built")
	var s stringer = &b
	fmt.Println(s.String())

	// `any` — псевдоним `interface{}`: методов нет,
	// поэтому ему удовлетворяет любой тип.
	values := []any{42, "go", Dog{"a"}, nil}
	for _, v := range values {
		fmt.Printf("%T ", v)
	}
	fmt.Println()

	// Интерфейсную переменную можно сравнить: равны,
	// если совпадают динамический тип и значение.
	var x, y Namer = Dog{"a"}, Dog{"a"}
	fmt.Println(x == y)
}
