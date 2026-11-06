// Pipeline — цепочка стадий, связанных каналами. Стадия —
// горутина, которая читает вход, обрабатывает значение и
// пишет в выход. Стадия владеет своим выходом: создаёт
// канал, пишет в него и закрывает его, когда работа кончилась.

package main

import (
	"context"
	"fmt"
	"slices"
	"sync"
)

// Источник превращает срез в канал. Отправка идёт через
// `select` с `ctx.Done()`: если потребитель ушёл и отменил
// контекст, горутина выйдет, а не повиснет на `out <- n`.
func gen(ctx context.Context, nums ...int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for _, n := range nums {
			select {
			case out <- n:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Промежуточная стадия устроена так же, только вход — канал.
// `range` по входу заканчивается, когда предыдущая стадия
// закрыла свой выход, — так закрытие идёт по цепочке.
func square(ctx context.Context, in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for n := range in {
			select {
			case out <- n * n:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out
}

// Fan-in сливает несколько каналов в один: по горутине
// на вход. Выход закрывает отдельная горутина после
// `wg.Wait()` — только она знает, что писателей больше нет.
func merge(ctx context.Context, chans ...<-chan int) <-chan int {
	out := make(chan int)
	var wg sync.WaitGroup
	for _, c := range chans {
		wg.Go(func() {
			for n := range c {
				select {
				case out <- n:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}

func main() {
	// Простая цепочка: gen → square → потребитель.
	// Порядок сохраняется: в каждой стадии одна горутина.
	ctx := context.Background()
	for n := range square(ctx, gen(ctx, 1, 2, 3)) {
		fmt.Println(n)
	}

	// Fan-out: три копии `square` читают один канал, и каждое
	// значение достаётся ровно одной из них. Fan-in собирает
	// результаты обратно. Порядок теперь зависит от
	// планировщика, поэтому для печати сортируем.
	src := gen(ctx, 1, 2, 3, 4, 5, 6)
	workers := []<-chan int{square(ctx, src), square(ctx, src), square(ctx, src)}
	var got []int
	for n := range merge(ctx, workers...) {
		got = append(got, n)
	}
	slices.Sort(got)
	fmt.Println(got)

	// Ранний выход: потребителю нужно одно значение. Без
	// отмены `gen` и `square` навсегда повисли бы на отправке.
	// `cancel()` закрывает `ctx.Done()`, стадии выходят и
	// закрывают свои каналы — `range` ниже заканчивается.
	ctx, cancel := context.WithCancel(context.Background())
	out := square(ctx, gen(ctx, 10, 20, 30, 40))
	fmt.Println("первое:", <-out)
	cancel()
	for range out {
	}
	fmt.Println("канал square закрыт, стадии остановлены")
}
