// Канал — это указатель на структуру `runtime.hchan`:
// кольцевой буфер, индексы `sendx`/`recvx`, очереди
// ждущих горутин `recvq`/`sendq` и мьютекс. Пример
// заглядывает внутрь через наблюдаемое поведение:
// аллокации, `len`, порядок пробуждения, закрытие.

package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

var sinkInt chan int
var sinkPtr chan *int

// waitBlocked ждёт, пока n горутин заснут в состоянии state.
// Дамп всех горутин показывает его в заголовке:
// `goroutine 7 [chan receive]:`.
func waitBlocked(state string, n int) {
	buf := make([]byte, 1<<16)
	for range 5000 {
		dump := string(buf[:runtime.Stack(buf, true)])
		if strings.Count(dump, "["+state) >= n {
			return
		}
		time.Sleep(time.Millisecond)
	}
	panic("горутины не заблокировались")
}

func main() {

	// `make` выделяет `hchan` в куче. Если элементы без
	// указателей, буфер идёт в той же noscan-аллокации; если с
	// указателями — отдельной, типизированной элементом:
	// по типу GC знает, где в буфере указатели.
	fmt.Println("аллокаций на make(chan int, 8): ", testing.AllocsPerRun(100, func() { sinkInt = make(chan int, 8) }))
	fmt.Println("аллокаций на make(chan *int, 8):", testing.AllocsPerRun(100, func() { sinkPtr = make(chan *int, 8) }))

	// Пока `qcount < dataqsiz`, отправка не блокируется:
	// значение копируется в `buf[sendx]`. `len` — это
	// `qcount`, `cap` — `dataqsiz`.
	ch := make(chan int, 3)
	ch <- 10
	ch <- 20
	fmt.Println("len, cap:", len(ch), cap(ch))
	<-ch
	<-ch

	// Получатели, которые ждут на пустом канале, стоят
	// в `recvq` в порядке прихода. Заблокируем трёх
	// по очереди и отправим 0, 1, 2.
	var mu sync.Mutex
	var wg sync.WaitGroup
	got := make([]string, 3)
	for i := range 3 {
		wg.Go(func() {
			v := <-ch
			mu.Lock()
			got[i] = fmt.Sprintf("r%d←%d", i, v)
			mu.Unlock()
		})
		waitBlocked("chan receive", i+1)
	}

	// Прямая передача: раз получатель ждёт, отправка кладёт
	// значение прямо в его стек, минуя буфер. Сразу после
	// отправки `len(ch)` по-прежнему 0.
	ch <- 0
	fmt.Println("len после отправки ждущему:", len(ch))
	ch <- 1
	ch <- 2
	wg.Wait()
	fmt.Println("FIFO среди ждущих:", strings.Join(got, " "))

	// `close` ставит `closed = 1` и будит всех из `recvq`
	// разом: каждый получает нулевое значение и `false`.
	done := make(chan bool, 3)
	for range 3 {
		go func() {
			_, ok := <-ch
			done <- ok
		}()
	}
	waitBlocked("chan receive", 3)
	close(ch)
	woken := 0
	for range 3 {
		if !<-done {
			woken++
		}
	}
	fmt.Println("close разбудил получателей:", woken)

	// Канал таймера с Go 1.23 выглядит небуферизованным:
	// `len` и `cap` у него всегда 0, даже когда таймер сработал.
	t := time.NewTimer(time.Millisecond)
	time.Sleep(5 * time.Millisecond)
	fmt.Println("канал таймера: len, cap:", len(t.C), cap(t.C))
	<-t.C
}
