// `select` ждёт сразу несколько операций с каналами
// и выполняет ту, что готова первой. Это главный
// инструмент координации горутин — аналог
// `Task.WhenAny` в C#, только для каналов.

package main

import (
	"fmt"
	"time"
)

func main() {

	// Два источника с разной задержкой — имитация
	// быстрого и медленного сервиса.
	fast := make(chan string)
	slow := make(chan string)

	go func() {
		time.Sleep(50 * time.Millisecond)
		fast <- "быстрый"
	}()
	go func() {
		time.Sleep(150 * time.Millisecond)
		slow <- "медленный"
	}()

	// `select` блокируется, пока одна из веток не станет
	// готова. Чтобы получить оба значения, ждём дважды.
	for range 2 {
		select {
		case msg := <-fast:
			fmt.Println("получено:", msg)
		case msg := <-slow:
			fmt.Println("получено:", msg)
		}
	}

	// `time.After` возвращает канал, в который придёт
	// значение через заданное время, — так делается таймаут.
	never := make(chan string)
	select {
	case msg := <-never:
		fmt.Println(msg)
	case <-time.After(100 * time.Millisecond):
		fmt.Println("таймаут")
	}

	// Ветка `default` делает `select` неблокирующим:
	// если ни один канал не готов, она выполняется сразу.
	select {
	case msg := <-never:
		fmt.Println(msg)
	default:
		fmt.Println("никто не готов")
	}

	// Отправку тоже можно сделать неблокирующей.
	// В буфере одно место: первая отправка пройдёт,
	// вторая уйдёт в `default`.
	buf := make(chan int, 1)
	for i := range 2 {
		select {
		case buf <- i:
			fmt.Println("отправлено:", i)
		default:
			fmt.Println("буфер полон, пропускаем:", i)
		}
	}

	// Если готовы несколько веток, Go выбирает случайную —
	// порядок `case` не задаёт приоритет. Проверим
	// на тысяче итераций, где готовы обе ветки сразу.
	a := make(chan struct{}, 1)
	b := make(chan struct{}, 1)
	wins := map[string]int{}
	for range 1000 {
		a <- struct{}{}
		b <- struct{}{}
		select {
		case <-a:
			wins["a"]++
			<-b
		case <-b:
			wins["b"]++
			<-a
		}
	}
	fmt.Println("выбирались обе ветки:", wins["a"] > 0 && wins["b"] > 0)
}
