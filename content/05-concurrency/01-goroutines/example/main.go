// Горутина — функция, которая выполняется конкурентно
// с остальной программой. Запускает её оператор `go`
// перед вызовом. Ни `async`, ни `Task`, ни пула потоков
// в коде нет: функция обычная, синхронная.

package main

import (
	"fmt"
	"runtime"
	"sync"
)

func square(i int, out []int, wg *sync.WaitGroup) {
	defer wg.Done() // сообщаем, что горутина закончила
	out[i] = i * i  // у каждой горутины своя ячейка — гонки нет
}

func main() {

	// `main` — тоже горутина. Пока других нет.
	fmt.Println("горутин в начале:", runtime.NumGoroutine())

	// `go f(args)` вычисляет аргументы сразу, запускает `f`
	// в новой горутине и сразу идёт дальше, не дожидаясь
	// результата. Вернуть значение через `go` нельзя —
	// результат пишем в общую память или отправляем в канал.
	//
	// Программа завершается, когда возвращается `main`,
	// а остальные горутины просто обрываются. Поэтому ждём
	// их явно: `sync.WaitGroup` — счётчик незавершённых задач
	// (подробно — в уроке про WaitGroup).
	out := make([]int, 5)
	var wg sync.WaitGroup
	for i := range 5 {
		wg.Add(1)
		go square(i, out, &wg)
	}
	wg.Wait()
	fmt.Println("квадраты:", out)

	// Чаще горутину запускают с замыканием. С Go 1.22
	// у каждой итерации своя `i`, так что её можно
	// захватывать без копирования в параметр.
	words := []string{"ноль", "один", "два"}
	upper := make([]int, len(words))
	for i, w := range words {
		wg.Add(1)
		go func() {
			defer wg.Done()
			upper[i] = len([]rune(w))
		}()
	}
	wg.Wait()
	fmt.Println("длины слов:", upper)

	// Горутина стоит дёшево: стек начинается с 2 КБ
	// и растёт по мере надобности. Сто тысяч горутин —
	// обычная нагрузка, а сто тысяч потоков ОС — нет.
	const n = 100_000
	done := make([]bool, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			done[i] = true
		}()
	}
	wg.Wait()
	count := 0
	for _, ok := range done {
		if ok {
			count++
		}
	}
	fmt.Println("отработало горутин:", count)

	// GOMAXPROCS — сколько потоков ОС одновременно выполняют
	// Go-код. По умолчанию — число доступных ядер.
	// Вызов с 0 только читает значение, с n > 0 — меняет его
	// и возвращает прежнее.
	prev := runtime.GOMAXPROCS(2)
	fmt.Println("GOMAXPROCS:", runtime.GOMAXPROCS(0))
	runtime.GOMAXPROCS(prev)
}
