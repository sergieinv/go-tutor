// Pipeline для живой визуализации: генератор → три воркера square
// (fan-out) → слияние merge (fan-in) → main. Меняйте число воркеров,
// задержку в square, буфер между стадиями или GOMAXPROCS и запускайте
// снова: схема строится по трассе настоящего запуска.
package main

import (
	"fmt"
	"sync"
	"time"
)

const (
	items   = 12
	workers = 3
	buffer  = 0 // попробуйте 4: генератор забежит вперёд воркеров
)

// gen — первая стадия: отдаёт числа и закрывает свой канал.
func gen(n int) <-chan int {
	out := make(chan int, buffer)
	go func() {
		defer close(out)
		for i := range n {
			out <- i // ждёт, пока свободный воркер заберёт значение
		}
	}()
	return out
}

// square — медленная стадия. Несколько её копий читают
// один входной канал — это fan-out.
func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			time.Sleep(2 * time.Millisecond) // «тяжёлая» работа
			out <- v * v
		}
	}()
	return out
}

// merge сливает каналы в один (fan-in): по горутине на вход,
// и ещё одна закрывает выход, когда все входы исчерпаны.
func merge(chans ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup
	for _, c := range chans {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for v := range c {
				out <- v
			}
		}()
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func main() {
	src := gen(items)
	var outs []<-chan int
	for range workers {
		outs = append(outs, square(src))
	}

	sum := 0
	for v := range merge(outs...) {
		sum += v
		// Попробуйте здесь break после первого значения:
		// итоговый шаг покажет застрявшие стадии — это утечка.
	}
	fmt.Println("сумма квадратов:", sum)
}
