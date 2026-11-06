// Канал годится не только для данных, но и для сигналов:
// «я закончил», «можно начинать», «твой ход». Само
// значение тогда не важно — важен момент его получения.

package main

import (
	"fmt"
	"unsafe"
)

// worker делает работу и сообщает о завершении в done.
// Тип `chan struct{}` говорит читателю кода: здесь сигнал,
// а не данные.
func worker(id int, log []string, done chan struct{}) {
	log[id] = fmt.Sprintf("worker %d: готово", id)
	done <- struct{}{}
}

// player отбивает мяч: получает счётчик, увеличивает
// и отдаёт партнёру. Пока мяч у одного игрока, второй
// ждёт — ходы строго чередуются. Получив мяч на счёте
// limit, игрок не бьёт, а отдаёт счёт в end.
func player(name string, limit int, in, out, end chan int) {
	for {
		ball := <-in
		if ball >= limit {
			end <- ball
			return
		}
		ball++
		fmt.Println(name, ball)
		out <- ball
	}
}

func main() {

	// `struct{}` — тип без полей. Его значения не занимают
	// памяти, а `struct{}{}` — единственное возможное значение.
	fmt.Println("размер struct{}:", unsafe.Sizeof(struct{}{}))

	// Ожидание одной горутины: `main` блокируется на получении,
	// пока worker не отправит сигнал. Всё, что worker записал
	// до отправки, `main` гарантированно увидит после получения.
	log := make([]string, 3)
	done := make(chan struct{})
	go worker(0, log, done)
	<-done
	fmt.Println(log[0])

	// Ожидание нескольких горутин — получить столько сигналов,
	// сколько горутин запущено. Для этого есть и готовый
	// инструмент — `sync.WaitGroup`.
	for id := 1; id < 3; id++ {
		go worker(id, log, done)
	}
	for range 2 {
		<-done
	}
	fmt.Println(log[1])
	fmt.Println(log[2])

	// Пинг-понг: два игрока передают мяч через пару
	// небуферизованных каналов. Канал здесь и данные
	// (счётчик), и эстафетная палочка: печатает только
	// тот, у кого сейчас мяч, поэтому вывод упорядочен.
	// Второй игрок в конце так и останется ждать мяча —
	// это утечка горутины; лечится она закрытием канала.
	ping, pong, end := make(chan int), make(chan int), make(chan int)
	go player("ping", 6, ping, pong, end)
	go player("pong", 6, pong, ping, end)

	ping <- 0 // подаём мяч
	fmt.Println("игра окончена на", <-end)
}
