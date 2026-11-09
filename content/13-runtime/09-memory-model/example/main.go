// Модель памяти отвечает на один вопрос: когда чтение
// в одной горутине гарантированно видит запись из другой.
// Ответ — когда запись **happens before** чтения. Это
// отношение складывается из порядка операций внутри
// горутины и рёбер синхронизации между горутинами:
// каналов, мьютексов, `Once`, атомиков, `WaitGroup`.
// Каждый блок ниже строит такое ребро явно; программа
// проходит `-race` и печатает одно и то же при любом
// планировании.
package main

import (
	"fmt"
	"sync"
	"sync/atomic"
)

type Config struct {
	Name  string
	Limit int
}

func main() {
	// **Канал.** Отправка synchronized before завершения
	// соответствующего получения. Запись в `msg` идёт до
	// отправки, печать — после получения, поэтому `msg`
	// гарантированно виден. Буфер не мешает: правило
	// «отправка → получение» верно для любой ёмкости.
	var msg string
	ch := make(chan struct{}, 1)
	go func() {
		msg = "привет из горутины"
		ch <- struct{}{}
	}()
	<-ch
	fmt.Println("канал:", msg)

	// **Буферизованный канал как семафор.** Правило
	// k-го получения: k-е получение из канала ёмкости C
	// synchronized before завершения (k+C)-й отправки.
	// Поэтому одновременно внутри — не больше C горутин,
	// и это гарантия модели памяти, а не удача.
	const C = 2
	sem := make(chan struct{}, C)
	var inside, peak atomic.Int32
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			sem <- struct{}{} // захватить место
			n := inside.Add(1)
			for {
				p := peak.Load()
				if n <= p || peak.CompareAndSwap(p, n) {
					break
				}
			}
			inside.Add(-1)
			<-sem // освободить место
		})
	}
	wg.Wait()
	fmt.Println("семафор: внутри не больше", C, "—", peak.Load() <= C)

	// **WaitGroup.** `Done` synchronized before возврата из
	// `Wait`, который он разблокировал. Каждая горутина
	// пишет в свой элемент среза обычной записью, а после
	// `Wait` главная горутина читает их все — без гонки.
	squares := make([]int, 5)
	for i := range squares {
		wg.Go(func() { squares[i] = i * i })
	}
	wg.Wait()
	fmt.Println("WaitGroup:", squares)

	// **Публикация через атомик.** Все операции
	// `sync/atomic` последовательно согласованы: если `Load`
	// увидел результат `Store`, то `Store` synchronized
	// before `Load`. Значит, читатель видит и все обычные
	// записи, сделанные до `Store`, — поля `Config`.
	var current atomic.Pointer[Config]
	published := make(chan struct{})
	go func() {
		c := &Config{Name: "prod", Limit: 100} // сначала заполнить
		current.Store(c)                       // потом опубликовать
		close(published)
	}()
	<-published
	if c := current.Load(); c != nil {
		fmt.Println("atomic.Pointer:", c.Name, c.Limit)
	}

	// **Флаг готовности.** Тот же принцип: обычная запись
	// `data`, затем атомарный `ready.Store(true)`. Читатель,
	// увидевший `true`, обязан увидеть и `data`. С обычным
	// `bool` это была бы гонка: ни видимость `data`, ни даже
	// выход из цикла не гарантированы.
	var data []string
	var ready atomic.Bool
	go func() {
		data = []string{"a", "b", "c"}
		ready.Store(true)
	}()
	for !ready.Load() {
		// ждём; в настоящем коде здесь был бы канал
	}
	fmt.Println("флаг готовности:", data)

	// **Once.** Завершение функции в `once.Do(f)`
	// synchronized before возврата из **любого** вызова
	// `Do`. Десять горутин зовут `Do` одновременно: `f`
	// выполняется один раз, и все видят её результат.
	var once sync.Once
	var cfg *Config
	var calls atomic.Int32
	seen := make([]string, 10)
	for i := range seen {
		wg.Go(func() {
			once.Do(func() {
				calls.Add(1)
				cfg = &Config{Name: "lazy", Limit: 7}
			})
			seen[i] = cfg.Name
		})
	}
	wg.Wait()
	fmt.Println("Once: вызовов", calls.Load(), "видели", seen[0], seen[9])
}
