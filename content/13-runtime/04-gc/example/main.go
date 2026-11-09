// Сборщик мусора Go — конкурентный mark-and-sweep без
// поколений и без перемещения объектов. Пример запускается
// с `GODEBUG=gctrace=1`: строки `gc N @...` в stderr —
// это трасса каждого цикла, её разбор — в тексте урока.
// Сам вывод программы от машины не зависит.

package main

import (
	"fmt"
	"runtime"
	"runtime/metrics"
	"time"
	"unsafe"
	"weak"
)

type node struct {
	next    *node
	payload [64]byte
}

// `heapLive` читает размер кучи живых объектов, найденных
// последней маркировкой, — из `runtime/metrics`, без
// остановки мира, которая нужна `runtime.ReadMemStats`.
func heapLive() uint64 {
	s := []metrics.Sample{{Name: "/gc/heap/live:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

// Список из `n` узлов: связанные указателями объекты кучи.
func build(n int) *node {
	var head *node
	for range n {
		head = &node{next: head}
	}
	return head
}

func main() {
	// Пока на список ссылается переменная — он достижим
	// от корня (стека `main`) и переживает сборку.
	list := build(100_000)
	runtime.GC()
	withList := heapLive()

	// Объекты в Go не перемещаются: адрес до и после
	// сборки один и тот же. В CLR сборщик уплотняет кучу
	// и двигает объекты, поэтому там нужен `fixed`.
	before := uintptr(unsafe.Pointer(list))
	runtime.GC()
	fmt.Println("адрес не изменился:", before == uintptr(unsafe.Pointer(list)))

	// Последнее использование `list` — выше. Дальше на
	// список никто не ссылается: маркировка его не найдёт,
	// а sweep вернёт память.
	runtime.GC()
	fmt.Println("живая куча уменьшилась:", heapLive() < withList)

	// `weak.Pointer` (Go 1.24) не держит объект: после
	// сборки `Value` возвращает nil.
	obj := &node{}
	w := weak.Make(obj)
	fmt.Println("пока объект жив:", w.Value() == obj)
	runtime.KeepAlive(obj)
	runtime.GC()
	fmt.Println("после сборки:", w.Value())

	// `runtime.AddCleanup` (Go 1.24) — замена финализатору:
	// функция получает не сам объект, а отдельный аргумент,
	// и запускается в отдельной горутине после сборки.
	done := make(chan string)
	file := &node{}
	runtime.AddCleanup(file, func(name string) { done <- name }, "data.bin")
	file = nil
	runtime.GC()
	select {
	case name := <-done:
		fmt.Println("cleanup закрыл", name)
	case <-time.After(5 * time.Second):
		fmt.Println("cleanup не запустился")
	}
}
