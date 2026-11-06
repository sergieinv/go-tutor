package task

import "sync"

// Map применяет f к каждому элементу in пулом из workers горутин.
// Порядок результатов совпадает с порядком in.
func Map[T, R any](in []T, workers int, f func(T) R) []R {
	out := make([]R, len(in)) // не nil даже для пустого in
	workers = max(workers, 1)

	// В канал задач идут индексы: по индексу воркер берёт
	// вход и пишет результат в свой элемент out — без гонок
	// и без отдельного канала результатов.
	jobs := make(chan int)
	var wg sync.WaitGroup
	for range min(workers, len(in)) {
		wg.Go(func() {
			for i := range jobs {
				out[i] = f(in[i])
			}
		})
	}
	for i := range in {
		jobs <- i
	}
	close(jobs)
	wg.Wait() // все воркеры вышли из range — горутин не осталось
	return out
}
