package task

import "sync"

// ParallelMap применяет f к каждому элементу in, каждый вызов —
// в своей горутине. out[i] == f(in[i]); возвращается после
// завершения всех вызовов.
func ParallelMap(in []int, f func(int) int) []int {
	// Срез выделяем заранее: каждая горутина пишет в свою
	// ячейку, поэтому гонки нет. append из горутин — гонка.
	out := make([]int, len(in))
	var wg sync.WaitGroup
	for i, v := range in {
		wg.Add(1)
		go func() {
			defer wg.Done()
			out[i] = f(v)
		}()
	}
	wg.Wait()
	return out
}
