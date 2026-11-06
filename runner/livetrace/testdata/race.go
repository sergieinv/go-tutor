// Гонка на счётчике — образец для тестов построителя race.
package main

import (
	"fmt"
	"sync"
)

func main() {
	hits := 0
	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			hits++
		}()
	}
	wg.Wait()
	fmt.Println(hits > 0)
}
