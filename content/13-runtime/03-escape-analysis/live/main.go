// Программа для живого визуализатора escape analysis. Песочница собирает её
// с -gcflags=-m, и каждое сообщение компилятора о main.go становится шагом:
// значение уходит в колонку «стек» или «куча». Меняйте код и запускайте снова:
// уберите //go:noinline, верните значение вместо указателя, замените
// fmt.Println на println — и посмотрите, как меняется решение компилятора.
package main

import "fmt"

type point struct{ x, y int }

var last *point // глобальная переменная живёт всю программу

// Возврат указателя на локальную переменную: p переживёт кадр функции,
// поэтому компилятор выделяет её в куче. //go:noinline запрещает встраивание,
// иначе в main решение приняли бы заново, по месту вызова.
//
//go:noinline
func newPoint(x, y int) *point {
	p := point{x, y}
	return &p
}

// Указатель только читают — он не убегает.
//
//go:noinline
func dist2(p *point) int { return p.x*p.x + p.y*p.y }

// Сохранение в глобальной переменной: параметр «утекает».
//
//go:noinline
func remember(p *point) { last = p }

// Замыкание возвращается из функции и уносит с собой n.
func counter() func() int {
	n := 0
	return func() int { n++; return n }
}

// Срез, который возвращают, живёт в куче.
func squares(n int) []int {
	s := make([]int, n)
	for i := range s {
		s[i] = i * i
	}
	return s
}

// Локальный буфер переменного размера: компилятор кладёт на стек
// до 32 байт, больше — выделит в куче уже во время работы.
func sumTo(n int) int {
	buf := make([]int, n)
	total := 0
	for i := range buf {
		buf[i] = i
		total += buf[i]
	}
	return total
}

// Слишком большой объект не помещается в кадр стека.
func zeros() int {
	buf := make([]byte, 1<<20)
	return len(buf)
}

func main() {
	a := newPoint(1, 2)
	remember(a)

	b := &point{3, 4} // адрес берут, но указатель не убегает
	d := dist2(b)

	// Маленькие функции компилятор встраивает (inlining) и решает
	// заново по месту вызова — здесь их результаты дальше main не уходят.
	next := counter()
	next()
	s := sumTo(4)   // 4 int = 32 байта: буфер на стеке
	q := squares(3) // срез больше не возвращают — тоже стек
	z := zeros()    // а 1 МБ в кадр не помещается и после встраивания

	// Аргументы fmt.Println упаковываются в any — и уходят в кучу.
	fmt.Println(d, s+len(q)+z, next())
}
