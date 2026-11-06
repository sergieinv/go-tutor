// Гонка данных — два доступа к одной переменной из разных
// горутин, хотя бы один из них запись, и между ними нет
// отношения happens-before. Здесь все способы такое
// отношение создать; гонку с отчётом `-race` смотрите
// в визуализаторе ниже.

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type config struct{ name string }

func main() {
	var wg sync.WaitGroup

	// Мьютекс: `Unlock` happens-before следующего `Lock`,
	// поэтому каждая горутина видит результат предыдущей.
	var mu sync.Mutex
	count := 0
	for range 100 {
		wg.Go(func() {
			mu.Lock()
			count++
			mu.Unlock()
		})
	}
	wg.Wait()
	fmt.Println("мьютекс:", count)

	// Канал: отправка happens-before соответствующего
	// получения. Всё, что горутина записала до `ch <- ...`,
	// получатель видит после `<-ch` — без мьютекса.
	cfg := &config{}
	ready := make(chan struct{})
	go func() {
		cfg.name = "prod"
		close(ready) // close тоже happens-before получения
	}()
	<-ready
	fmt.Println("канал:", cfg.name)

	// `WaitGroup`: `Done` happens-before возврата из `Wait`.
	// Горутины пишут в разные элементы среза — это разные
	// переменные, гонки между ними нет.
	squares := make([]int, 5)
	for i := range squares {
		wg.Go(func() { squares[i] = i * i })
	}
	wg.Wait()
	fmt.Println("WaitGroup:", squares)

	// `sync.Once`: функция выполняется один раз, и её
	// завершение happens-before возврата из любого `Do`.
	var once sync.Once
	var inits atomic.Int32
	var shared *config
	for range 10 {
		wg.Go(func() {
			once.Do(func() {
				inits.Add(1)
				shared = &config{name: "lazy"}
			})
			_ = shared.name // безопасно: Do уже вернулся
		})
	}
	wg.Wait()
	fmt.Println("Once:", inits.Load(), shared.name)

	// Атомарные операции тоже синхронизируют. Но без гонки
	// данных ещё не значит без race condition: `Load`, затем
	// `Store` — каждый атомарен, а вместе нет, и обновления
	// теряются. `CompareAndSwap` в цикле повторяет попытку,
	// если значение успели поменять.
	var balance atomic.Int64
	for range 100 {
		wg.Go(func() {
			for {
				old := balance.Load()
				if balance.CompareAndSwap(old, old+10) {
					return
				}
			}
		})
	}
	wg.Wait()
	fmt.Println("CAS:", balance.Load())
}
