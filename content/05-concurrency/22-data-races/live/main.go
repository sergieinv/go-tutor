// Программа с двумя гонками данных. Визуализатор запускает её с -race
// и строит шаги по отчёту детектора. Почините гонки — оберните обе
// строки, помеченные «гонка», в mu.Lock() / mu.Unlock() — и запустите
// снова: детектор должен сообщить, что гонок нет.
package main

import (
	"fmt"
	"sync"
)

// mu пока никто не использует.
var mu sync.Mutex

func main() {
	var (
		total int
		log   []string
		wg    sync.WaitGroup
	)

	for i := range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			total += i                                  // гонка: запись без синхронизации
			log = append(log, fmt.Sprint("воркер ", i)) // гонка: общий срез
		}()
	}

	// wg.Wait() — это happens-before: после него main видит
	// все записи воркеров, и чтения ниже безопасны.
	wg.Wait()
	fmt.Println("total:", total, "записей:", len(log))
}
