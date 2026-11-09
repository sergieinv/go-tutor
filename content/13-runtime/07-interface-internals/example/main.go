// Интерфейсное значение — два машинных слова. Пример заглядывает
// внутрь: что лежит в словах, когда упаковка в интерфейс выделяет
// память, а когда нет, чем грозит сравнение интерфейсов и как
// компилятор убирает косвенный вызов (смотрите вывод `-gcflags=-m`
// под программой: строки `devirtualizing` и `escapes to heap`).

package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"
	"unsafe"
)

type Shape interface{ Area() int }

type Square struct{ Side int }

func (s Square) Area() int { return s.Side * s.Side }

type Rect struct{ W, H int }

func (r *Rect) Area() int { return r.W * r.H }

// Приёмник и исходные значения — глобальные: иначе компилятор
// увидит константы и подставит готовые статические данные.
var (
	sink  any
	small = 7
	big   = 1000
	flag  = true
	word  = "gopher"
	empty = ""
	tiny  = Square{Side: 3}
	sq    = Square{Side: 300}
	rect  = &Rect{W: 2, H: 5}
	none  struct{}
)

func main() {
	// И `any` (`eface`: тип + данные), и непустой интерфейс
	// (`iface`: itab + данные) занимают два слова.
	fmt.Println("sizeof any, io.Writer:", unsafe.Sizeof(sink), unsafe.Sizeof(io.Writer(nil)))

	// Только чтобы заглянуть внутрь: раскладка интерфейса — деталь
	// рантайма, в рабочем коде так не делают. Указатель кладётся
	// прямо во второе слово, без копии и без аллокации.
	var e any = rect
	words := (*[2]unsafe.Pointer)(unsafe.Pointer(&e))
	fmt.Println("data word is the pointer itself:", words[1] == unsafe.Pointer(rect))

	// Не-указатель копируется, а второе слово указывает на копию.
	// Аллокации нет, если копия не нужна в куче: маленькие целые
	// (0–255) и bool берутся из статической таблицы, пустая строка
	// и значения нулевого размера — из общих нулевых данных.
	// Структура из одного поля упаковывается как само поле:
	// `Square{3}` — как число 3.
	allocs := func(name string, f func()) {
		fmt.Printf("allocs %-14s %v\n", name+":", testing.AllocsPerRun(100, f))
	}
	allocs("int 7", func() { sink = small })
	allocs("int 1000", func() { sink = big })
	allocs("const 1000", func() { sink = 1000 })
	allocs("bool", func() { sink = flag })
	allocs(`""`, func() { sink = empty })
	allocs(`"gopher"`, func() { sink = word })
	allocs("struct{}", func() { sink = none })
	allocs("Square{300}", func() { sink = sq })
	allocs("Square{3}", func() { sink = tiny })
	allocs("*Rect", func() { sink = rect })

	// Упакованная копия независима от оригинала — как boxing в C#.
	var boxed Shape = sq
	sq.Side = 10
	fmt.Println("boxed copy:", boxed.Area(), "original:", sq.Area())

	// Вызов через интерфейс — косвенный: адрес метода берётся из
	// таблицы `itab.Fun`. Но здесь компилятор знает динамический тип
	// и заменяет вызов прямым (строка `devirtualizing` в выводе -m).
	var s Shape = Square{Side: 4}
	fmt.Println("area:", s.Area())

	// Type assertion к конкретному типу сравнивает указатель на тип,
	// к интерфейсу — ищет itab (с кешем на месте вызова).
	var w io.Writer = &strings.Builder{}
	_, isBuilder := w.(*strings.Builder)
	_, isStringer := w.(fmt.Stringer)
	_, isReader := w.(io.Reader)
	fmt.Println("assert:", isBuilder, isStringer, isReader)

	// Интерфейсы равны, если равны динамические типы и значения.
	// Разные типы — просто false, а одинаковый несравнимый тип
	// (срез, map, функция) — паника во время выполнения.
	fmt.Println("1 == \"1\":", any(1) == any("1"))
	fmt.Println("compare slices:", compare([]int{1}, []int{1}))

	// Интерфейс равен nil, только если пусты оба слова.
	var r *Rect
	var sh Shape = r
	fmt.Println("nil pointer inside:", sh == nil, sh != nil)

	// errors.Is сравнивает ошибки через `==` — и не паникует:
	// перед сравнением проверяет, сравним ли тип.
	fmt.Println("errors.Is:", errors.Is(io.EOF, io.EOF))
}

// compare сравнивает два интерфейса и превращает панику в текст.
func compare(a, b any) (res string) {
	defer func() {
		if p := recover(); p != nil {
			res = fmt.Sprint("panic: ", p)
		}
	}()
	return fmt.Sprint(a == b)
}
