// Буферизованный канал хранит до `cap` значений.
// Отправка блокируется, только когда буфер полон,
// получение — только когда он пуст. Отправитель
// и получатель больше не обязаны встречаться.

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

func main() {

	// Ёмкость — второй аргумент `make`. В канал на 3
	// элемента можно отправить три значения без получателя,
	// даже в одной горутине.
	queue := make(chan string, 3)
	queue <- "a"
	queue <- "b"

	// `len` — сколько значений сейчас в буфере,
	// `cap` — его ёмкость. Для небуферизованного канала
	// обе функции вернут 0.
	fmt.Println("len:", len(queue), "cap:", cap(queue))

	queue <- "c"
	// Четвёртая отправка здесь заблокировала бы `main`
	// навсегда: буфер полон, а получать некому.
	fmt.Println(<-queue, <-queue, <-queue) // FIFO

	// Буфер — очередь между производителем и потребителем:
	// производитель может забежать вперёд на `cap` значений
	// и не ждать каждого получения.
	jobs := make(chan int, 5)
	go func() {
		for i := 1; i <= 5; i++ {
			jobs <- i * i // ни одна отправка не ждёт
		}
	}()
	sum := 0
	for range 5 {
		sum += <-jobs
	}
	fmt.Println("сумма квадратов:", sum)

	// Буфер — семафор: канал ёмкостью N пропускает
	// не больше N горутин одновременно. Занять слот —
	// отправить, освободить — получить.
	sem := make(chan struct{}, 3)
	var running atomic.Int32 // сколько горутин сейчас в «зоне»
	var exceeded atomic.Bool
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}        // ждём свободного слота
			defer func() { <-sem }() // освобождаем слот

			if running.Add(1) > 3 {
				exceeded.Store(true)
			}
			running.Add(-1)
		}()
	}
	wg.Wait()
	fmt.Println("лимит 3 превышен:", exceeded.Load())
	fmt.Println("в семафоре после работы:", len(sem))
}
