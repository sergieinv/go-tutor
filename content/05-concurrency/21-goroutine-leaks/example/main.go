// Горутина живёт, пока не вернётся её функция. Сборщик мусора
// горутины не останавливает: если она ждёт канал, который
// никто не прочтёт, она висит до конца процесса вместе со
// всем, на что ссылается. Это утечка горутины.

package main

import (
	"context"
	"fmt"
	"runtime"
	"time"
)

// Классическая утечка: результат ждут с таймаутом через
// небуферизованный канал. После таймаута получателя нет,
// и горутина навсегда застревает на `ch <- 42`.
// Исправление — буфер на одно значение: отправка пройдёт
// без получателя, горутина завершится, а канал соберёт GC.
func fetch(buf int) bool {
	ch := make(chan int, buf)
	go func() {
		time.Sleep(50 * time.Millisecond) // медленный бэкенд
		ch <- 42
	}()
	select {
	case <-ch:
		return true
	case <-time.After(time.Millisecond):
		return false
	}
}

// Забытый `close`: читатель в `range` ждёт следующего
// значения вечно, если отправитель не закрыл канал.
func produce(closeIt bool) {
	ch := make(chan int)
	go func() {
		for range ch {
		}
	}()
	for i := range 3 {
		ch <- i
	}
	if closeIt {
		close(ch)
	}
}

// Горутина с вечным циклом должна иметь условие выхода —
// обычно `ctx.Done()`. `defer t.Stop()` освобождает тикер.
func poll(ctx context.Context) {
	t := time.NewTicker(time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-t.C:
		case <-ctx.Done():
			return
		}
	}
}

// `runtime.NumGoroutine` — самый простой детектор: сколько
// горутин живо сверх базы. Завершающимся горутинам нужно
// время, поэтому ждём, пока число не опустится, но не вечно.
func extra(base int) int {
	deadline := time.Now().Add(500 * time.Millisecond)
	for runtime.NumGoroutine() > base && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	return runtime.NumGoroutine() - base
}

func main() {
	base := runtime.NumGoroutine()
	for range 3 {
		fetch(0)
	}
	fmt.Println("fetch без буфера, утекло:", extra(base))

	base = runtime.NumGoroutine()
	for range 3 {
		fetch(1)
	}
	fmt.Println("fetch с буфером 1, утекло:", extra(base))

	base = runtime.NumGoroutine()
	produce(false)
	fmt.Println("range без close, утекло:", extra(base))
	produce(true)
	fmt.Println("range с close, утекло ещё:", extra(base+1))

	base = runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	go poll(ctx)
	cancel()
	fmt.Println("poll после cancel, утекло:", extra(base))
}
