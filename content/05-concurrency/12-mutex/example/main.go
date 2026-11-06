// `sync.Mutex` защищает данные, которые меняют несколько
// горутин. Мьютекс кладут в структуру рядом с полями,
// которые он охраняет, а методы берут его на время
// работы с ними.

package main

import (
	"fmt"
	"sync"
)

// Счётчики по ключам. Мьютекс стоит прямо над полями,
// которые защищает, — так принято писать в Go. Нулевое
// значение `Mutex` — открытый замок, конструктор не нужен.
type Stats struct {
	mu     sync.Mutex
	counts map[string]int
}

// Методы с мьютексом — только с указателем-получателем:
// копия структуры скопировала бы и мьютекс.
func (s *Stats) Inc(key string) {
	s.mu.Lock()
	// `defer` отпустит мьютекс при любом выходе из метода,
	// в том числе при панике.
	defer s.mu.Unlock()
	s.counts[key]++
}

func (s *Stats) Get(key string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.counts[key]
}

// Инвариант из нескольких полей: сумма на двух счетах
// постоянна. Оба поля меняются под одним замком —
// никто не увидит деньги «в полёте».
type Accounts struct {
	mu   sync.Mutex
	a, b int
}

func (x *Accounts) Move(n int) {
	x.mu.Lock()
	defer x.mu.Unlock()
	x.a -= n
	x.b += n
}

func (x *Accounts) Total() int {
	x.mu.Lock()
	defer x.mu.Unlock()
	return x.a + x.b
}

// `RWMutex` пускает много читателей одновременно,
// а писателя — только одного и без читателей.
type Cache struct {
	mu   sync.RWMutex
	data map[string]string
}

func (c *Cache) Get(k string) (string, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	v, ok := c.data[k]
	return v, ok
}

func (c *Cache) Set(k, v string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.data[k] = v
}

func main() {
	stats := &Stats{counts: map[string]int{}}
	acc := &Accounts{a: 1000}
	cache := &Cache{data: map[string]string{"lang": "Go"}}

	// Сто горутин одновременно пишут во все три структуры.
	// Без мьютексов `-race` сразу нашёл бы гонки, а запись
	// в map из разных горутин уронила бы программу.
	var wg sync.WaitGroup
	for i := range 100 {
		wg.Go(func() {
			stats.Inc("get")
			if i%4 == 0 {
				stats.Inc("set")
				cache.Set("mode", "rw")
			}
			acc.Move(1)
			cache.Get("lang")
		})
	}
	wg.Wait()

	fmt.Println("get:", stats.Get("get"), "set:", stats.Get("set"))
	fmt.Println("сумма на счетах:", acc.Total())
	v, _ := cache.Get("mode")
	fmt.Println("mode:", v)
}
