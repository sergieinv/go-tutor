package task

import "sync"

// RunLimited выполняет все задачи конкурентно, но не больше
// limit одновременно. Возвращается после завершения всех задач.
func RunLimited(tasks []func(), limit int) {
	limit = max(limit, 1)
	// Канал-семафор: в буфере limit слотов.
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for _, task := range tasks {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}        // занять слот: ждёт, если заняты все
			defer func() { <-sem }() // освободить слот
			task()
		}()
	}
	wg.Wait()
}
