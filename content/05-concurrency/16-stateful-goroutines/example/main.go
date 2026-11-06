// Состоянием может владеть одна горутина: только она
// читает и меняет данные, а остальные шлют ей запросы
// по каналам. Это и есть «не общайтесь через общую
// память — делитесь памятью, общаясь».

package main

import (
	"fmt"
	"sync"
)

// Запрос на чтение несёт канал для ответа — так
// горутина-владелец знает, куда вернуть значение.
type readOp struct {
	key  string
	resp chan int
}

// Запрос на изменение тоже ждёт ответа: отправитель
// должен знать, что запись уже применена.
type addOp struct {
	key   string
	delta int
	resp  chan struct{}
}

// counters — ручки для общения с владельцем.
// Карта `state` живёт внутри горутины, снаружи её не видно.
type counters struct {
	reads chan readOp
	adds  chan addOp
	quit  chan struct{}
}

func start() counters {
	c := counters{make(chan readOp), make(chan addOp), make(chan struct{})}
	go func() {
		state := map[string]int{}
		for {
			// Запросы обрабатываются строго по одному —
			// мьютекс не нужен: к карте ходит одна горутина.
			select {
			case r := <-c.reads:
				r.resp <- state[r.key]
			case a := <-c.adds:
				state[a.key] += a.delta
				a.resp <- struct{}{}
			case <-c.quit:
				return
			}
		}
	}()
	return c
}

// Методы прячут каналы за обычным API.
func (c counters) add(key string, delta int) {
	resp := make(chan struct{})
	c.adds <- addOp{key, delta, resp}
	<-resp
}

func (c counters) get(key string) int {
	resp := make(chan int)
	c.reads <- readOp{key, resp}
	return <-resp
}

func main() {
	c := start()
	defer close(c.quit)

	// Десять горутин по сто раз увеличивают счётчики.
	// Гонки нет: все изменения проходят через владельца.
	var wg sync.WaitGroup
	for i := range 10 {
		wg.Go(func() {
			key := []string{"чёт", "нечет"}[i%2]
			for range 100 {
				c.add(key, 1)
			}
		})
	}
	wg.Wait()
	fmt.Println("чёт:", c.get("чёт"), "нечет:", c.get("нечет"))

	// Тот же счётчик на мьютексе — короче. Для простого
	// состояния без сложной логики мьютекс обычно лучше.
	var mu sync.Mutex
	state := map[string]int{}
	for i := range 10 {
		wg.Go(func() {
			mu.Lock()
			defer mu.Unlock()
			state[[]string{"чёт", "нечет"}[i%2]] += 100
		})
	}
	wg.Wait()
	fmt.Println("с мьютексом:", state["чёт"], state["нечет"])
}
