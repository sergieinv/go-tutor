// Пакет `sync/atomic` даёт операции, которые процессор
// выполняет неделимо: чтение, запись, сложение, обмен
// и сравнение-с-обменом (CAS). Для одного счётчика или
// флага это дешевле мьютекса.

package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

// Неизменяемая конфигурация: её не правят на месте,
// а целиком заменяют новой.
type Config struct {
	Version int
	Limit   int
}

func main() {
	// Типы `atomic.Int64`, `atomic.Bool`, `atomic.Pointer[T]`
	// (Go 1.19+) удобнее старых функций `atomic.AddInt64(&x, 1)`:
	// к значению нельзя случайно обратиться без atomic.
	// Нулевое значение готово к работе.
	var requests atomic.Int64

	var wg sync.WaitGroup
	for range 50 {
		wg.Go(func() {
			for range 100 {
				requests.Add(1)
			}
		})
	}
	wg.Wait()
	fmt.Println("запросов:", requests.Load())

	// `CompareAndSwap(old, new)` меняет значение, только если
	// оно всё ещё равно `old`. Из десяти горутин «закрыть»
	// сможет ровно одна.
	var closed atomic.Bool
	var winners atomic.Int32
	for range 10 {
		wg.Go(func() {
			if closed.CompareAndSwap(false, true) {
				winners.Add(1)
			}
		})
	}
	wg.Wait()
	fmt.Println("закрыто:", closed.Load(), "закрывших:", winners.Load())

	// CAS-цикл: прочитать, вычислить новое значение,
	// попытаться записать; если кто-то успел раньше —
	// повторить. Так обновляют максимум без мьютекса.
	var maxSeen atomic.Int64
	for _, v := range []int64{17, 42, 8, 99, 23} {
		wg.Go(func() {
			for {
				cur := maxSeen.Load()
				if v <= cur || maxSeen.CompareAndSwap(cur, v) {
					return
				}
			}
		})
	}
	wg.Wait()
	fmt.Println("максимум:", maxSeen.Load())

	// `atomic.Pointer[T]` атомарно подменяет указатель.
	// Читатели получают либо старую, либо новую конфигурацию
	// целиком — но никогда смесь полей.
	var cfg atomic.Pointer[Config]
	cfg.Store(&Config{Version: 1, Limit: 10})
	old := cfg.Swap(&Config{Version: 2, Limit: 20})
	cur := cfg.Load()
	fmt.Println("было:", *old, "стало:", *cur)
}
