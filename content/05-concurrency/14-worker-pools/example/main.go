// Пул воркеров — N горутин, которые разбирают задачи
// из общего канала. Число горутин фиксировано, поэтому
// фиксирован и параллелизм: тысяча задач не превратится
// в тысячу одновременных запросов к базе.

package main

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
)

// Задача несёт свой индекс: результаты приходят
// в произвольном порядке, а индекс позволяет разложить
// их по местам.
type job struct {
	index int
	word  string
}

type result struct {
	index int
	value string
}

func main() {
	words := []string{"go", "channel", "worker", "pool", "select", "mutex"}

	jobs := make(chan job)
	results := make(chan result)

	// Запускаем трёх воркеров. Каждый читает `jobs`, пока
	// канал не закрыт: `range` по каналу завершится сам.
	const workers = 3
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for j := range jobs {
				results <- result{j.index, strings.ToUpper(j.word)}
			}
		})
	}

	// Отправитель закрывает `jobs`, когда задачи кончились, —
	// это сигнал воркерам «больше ничего не будет».
	go func() {
		for i, w := range words {
			jobs <- job{i, w}
		}
		close(jobs)
	}()

	// `results` закрывает тот, кто знает, что писателей больше
	// нет: отдельная горутина дожидается всех воркеров.
	go func() {
		wg.Wait()
		close(results)
	}()

	// Собираем результаты по индексу — порядок восстановлен,
	// хотя воркеры заканчивали в каком угодно порядке.
	out := make([]string, len(words))
	for r := range results {
		out[r.index] = r.value
	}
	fmt.Println(out)

	// Без явного пула: горутина на задачу, а параллелизм
	// ограничивает семафор — буферизованный канал на N мест.
	// Занять место — отправить в канал, освободить — прочитать.
	sem := make(chan struct{}, 2)
	var active atomic.Int32
	var overflow atomic.Bool
	lengths := make([]int, len(words))
	var wg2 sync.WaitGroup
	for i, w := range words {
		wg2.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()

			if active.Add(1) > 2 {
				overflow.Store(true)
			}
			lengths[i] = len(w) // каждая горутина пишет в свой элемент — гонки нет
			active.Add(-1)
		})
	}
	wg2.Wait()
	fmt.Println(lengths)
	fmt.Println("больше двух одновременно:", overflow.Load())
}
