// `defer` откладывает вызов функции до момента, когда
// окружающая функция завершится — обычным `return`
// или паникой. Это замена `finally` и `using`: ресурс
// закрывают сразу под строкой, где его открыли.

package main

import (
	"errors"
	"fmt"
)

// Ресурс-заглушка вместо файла: `Close` тоже может
// вернуть ошибку — как `*os.File` при сбросе буфера.
type resource struct {
	name     string
	closeErr error
}

func open(name string) *resource {
	fmt.Println("open", name)
	return &resource{name: name}
}

func (r *resource) Close() error {
	fmt.Println("close", r.name)
	return r.closeErr
}

// Отложенные вызовы выполняются в обратном порядке —
// как стек (LIFO): последним открыли, первым закрыли.
func lifo() {
	a := open("a")
	defer a.Close()
	b := open("b")
	defer b.Close()
	fmt.Println("работаем с a и b")
}

// Аргументы отложенного вызова вычисляются сразу,
// в строке с `defer`, а сам вызов — в конце функции.
func args() {
	x := 1
	defer fmt.Println("defer видит x =", x)
	x = 2
	fmt.Println("в конце x =", x)
}

// Отложенное замыкание может изменить именованный
// результат уже после `return`. Так не теряют ошибку
// `Close`: она важна, если основная работа прошла.
func write(r *resource) (err error) {
	defer func() {
		if cerr := r.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close %s: %w", r.name, cerr)
		}
	}()
	fmt.Println("write", r.name)
	return nil
}

func main() {
	lifo()
	args()

	err := write(&resource{name: "log", closeErr: errors.New("disk full")})
	fmt.Println("write вернул:", err)

	// `defer` привязан к функции, а не к блоку. В цикле
	// отложенные вызовы копятся до выхода из функции.
	// Чтобы закрывать на каждой итерации, тело выносят
	// в отдельную функцию — здесь анонимную.
	for _, name := range []string{"x", "y"} {
		func() {
			r := open(name)
			defer r.Close()
			fmt.Println("обработка", name)
		}()
	}
}
