// `close(ch)` сообщает получателям: значений больше
// не будет. Закрытие — не уничтожение канала, а сигнал
// «конец потока», который видят все получатели.

package main

import (
	"fmt"
	"sync"
)

// produce отправляет числа и закрывает канал. Закрывает
// всегда отправитель: только он знает, что данных больше нет.
func produce(n int, out chan int) {
	defer close(out)
	for i := 1; i <= n; i++ {
		out <- i * 10
	}
}

// trySend показывает, что будет при отправке в закрытый канал.
func trySend(ch chan int) {
	defer func() { fmt.Println("паника:", recover()) }()
	ch <- 1
}

func main() {

	// `v, ok := <-ch` отличает значение от закрытия:
	// `ok == false` — канал закрыт и пуст, `v` — нулевое
	// значение типа.
	ch := make(chan int)
	go produce(2, ch)
	for {
		v, ok := <-ch
		if !ok {
			fmt.Println("канал закрыт, v =", v)
			break
		}
		fmt.Println("получено", v)
	}

	// `for v := range ch` делает то же короче: цикл
	// заканчивается, когда канал закрыт и все значения
	// из него получены.
	ch2 := make(chan int)
	go produce(3, ch2)
	for v := range ch2 {
		fmt.Println("range:", v)
	}

	// Значения, уже лежащие в буфере, закрытие не теряет:
	// получатель сначала дочитает их, и только потом
	// увидит `ok == false`.
	buf := make(chan string, 2)
	buf <- "a"
	buf <- "b"
	close(buf)
	for s := range buf {
		fmt.Println("из буфера:", s)
	}

	// Закрытие — широковещательный сигнал: получение из
	// закрытого канала сразу завершается у всех горутин.
	// Отправкой так не сделать — её получит только одна.
	start := make(chan struct{})
	results := make([]string, 3)
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start // ждём общего старта
			results[i] = fmt.Sprint("бегун ", i, " стартовал")
		}()
	}
	close(start) // будит всех троих
	wg.Wait()
	for _, r := range results {
		fmt.Println(r)
	}

	// Отправка в закрытый канал — паника. Повторный
	// `close` — тоже.
	closed := make(chan int, 1)
	close(closed)
	trySend(closed)
}
