// Метод — функция с получателем. Получатель указывается
// перед именем метода и бывает значением или указателем.

package main

import (
	"fmt"
	"strings"
)

// Методы можно объявить на любом именованном типе,
// даже на числе. `Celsius` и `float64` — разные типы.
type Celsius float64

// Value receiver: метод получает копию значения.
// Подходит для маленьких неизменяемых типов.
func (c Celsius) Fahrenheit() float64 {
	return float64(c)*9/5 + 32
}

type Counter struct {
	name string
	hits int
}

// Конструкторов нет — есть функции `NewX`. Возвращать
// указатель принято, когда у типа pointer-методы.
func NewCounter(name string) *Counter {
	return &Counter{name: strings.ToUpper(name)}
}

// Pointer receiver: метод меняет исходную структуру.
func (c *Counter) Hit() {
	c.hits++
}

// Этот метод только читает, но у типа уже есть
// pointer-метод — делаем указателями все.
func (c *Counter) String() string {
	return fmt.Sprintf("%s=%d", c.name, c.hits)
}

// Метод с value receiver на структуре: меняет копию.
type Box struct{ n int }

func (b Box) SetBroken(n int) { b.n = n }
func (b *Box) Set(n int)      { b.n = n }

func main() {
	fmt.Println(Celsius(100).Fahrenheit())

	c := NewCounter("home")
	c.Hit()
	c.Hit()
	fmt.Println(c.String())

	// На переменной-значении pointer-метод вызывается
	// напрямую: компилятор сам возьмёт адрес, `(&v).Hit()`.
	var v Counter
	v.Hit()
	fmt.Println(v.hits)

	// И наоборот: value-метод на указателе — Go разыменует.
	t := new(Celsius)
	*t = 37
	fmt.Printf("%.1f\n", t.Fahrenheit())

	// Value receiver не меняет исходное значение.
	b := Box{}
	b.SetBroken(1)
	fmt.Println(b.n)
	b.Set(2)
	fmt.Println(b.n)

	// Method value: метод, привязанный к получателю,
	// можно сохранить в переменную как обычную функцию.
	hit := c.Hit
	hit()
	fmt.Println(c)

	// Method expression: метод как функция, где
	// получатель — первый аргумент.
	f := (*Counter).Hit
	f(c)
	fmt.Println(c)
}
