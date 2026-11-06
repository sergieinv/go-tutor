// Встраивание — поле без имени. Его поля и методы
// продвигаются во внешнюю структуру. Это композиция:
// внешний тип содержит встроенный, а не является им.

package main

import (
	"fmt"
	"io"
	"strings"
)

type User struct {
	Name string
}

func (u User) Greet() string { return "hi, " + u.Name }
func (u User) Role() string  { return "user" }

// `Describe` вызывает `u.Role()` — это всегда `User.Role`.
func (u User) Describe() string { return u.Name + " is " + u.Role() }

// `Admin` встраивает `User`: имя поля совпадает с именем
// типа — `a.User`.
type Admin struct {
	User
	Level int
}

// Метод с тем же именем затеняет `User.Role` снаружи.
func (a Admin) Role() string { return "admin" }

type Greeter interface{ Greet() string }

// Интерфейсы тоже встраиваются: `ReadCounter` — это
// `io.Reader` плюс ещё один метод.
type ReadCounter interface {
	io.Reader
	Count() int
}

// Встраивание интерфейса в структуру: все методы
// `io.Reader` пересылаются в поле, а `Read` мы
// переопределяем, чтобы считать байты.
type countingReader struct {
	io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.Reader.Read(p)
	c.n += n
	return n, err
}

func (c *countingReader) Count() int { return c.n }

func main() {
	a := Admin{User: User{Name: "ann"}, Level: 2}

	// Продвинутые поле и метод.
	fmt.Println(a.Name, a.Greet())

	// Явный путь к встроенному значению тоже работает.
	fmt.Println(a.User.Name == a.Name)

	// Снаружи `Role` — метод `Admin`, а `a.User.Role()`
	// по-прежнему доступен.
	fmt.Println(a.Role(), a.User.Role())

	// Главное отличие от наследования: `Describe` —
	// метод `User`, и он вызывает `User.Role`, а не
	// `Admin.Role`. Виртуальной диспетчеризации нет.
	fmt.Println(a.Describe())

	// Продвинутые методы входят в method set, поэтому
	// `Admin` реализует `Greeter`.
	var g Greeter = a
	fmt.Println(g.Greet())

	// Но `Admin` не является `User`: `var u User = a`
	// не скомпилируется. Нужно взять поле явно.
	var u User = a.User
	fmt.Println(u.Role())

	var rc ReadCounter = &countingReader{Reader: strings.NewReader("hello, go")}
	data, _ := io.ReadAll(rc)
	fmt.Println(string(data), rc.Count())
}
