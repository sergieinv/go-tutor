// Программа для живой визуализации select. Меняйте задержки,
// размер буферов, число итераций или GOMAXPROCS и запускайте снова:
// схема строится по трассе настоящего запуска.
package main

import (
	"fmt"
	"time"
)

func main() {
	fast := make(chan string)
	slow := make(chan string)

	go func() {
		time.Sleep(10 * time.Millisecond)
		fast <- "быстрый"
	}()
	go func() {
		time.Sleep(60 * time.Millisecond)
		slow <- "медленный"
	}()

	// Ни одна ветка не готова: main засыпает в select,
	// пока его не разбудит отправка или таймер.
	for range 2 {
		select {
		case msg := <-fast:
			fmt.Println("получено:", msg)
		case msg := <-slow:
			fmt.Println("получено:", msg)
		case <-time.After(30 * time.Millisecond):
			fmt.Println("таймаут")
		}
	}

	// default: ничего не готово — select не ждёт.
	select {
	case msg := <-slow:
		fmt.Println("получено:", msg)
	default:
		fmt.Println("медленный ещё в пути")
	}

	// Обе ветки готовы — выбор случаен, смотрите на счётчики.
	a := make(chan int, 1)
	b := make(chan int, 1)
	for range 10 {
		a <- 1
		b <- 2
		select {
		case <-a:
			<-b
		case <-b:
			<-a
		}
	}
}
