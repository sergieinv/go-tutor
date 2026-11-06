package task

import (
	"context"
	"sync"
	"time"
)

// Код ниже работает правильно, но оставляет висящие горутины.
// Найдите утечки и почините их, не меняя сигнатуры и поведение.

// Lookup вызывает get для всех ключей одновременно и собирает
// ответы в map. Если ctx отменён раньше, чем пришли все ответы,
// возвращает nil и ctx.Err(), не дожидаясь get.
func Lookup(ctx context.Context, keys []string, get func(string) string) (map[string]string, error) {
	type pair struct{ key, value string }
	replies := make(chan pair)
	for _, k := range keys {
		go func() {
			replies <- pair{k, get(k)}
		}()
	}

	out := make(map[string]string, len(keys))
	for range keys {
		select {
		case p := <-replies:
			out[p.key] = p.value
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	return out, nil
}

// Total считает weight для всех чисел одновременно и возвращает
// сумму весов.
func Total(nums []int, weight func(int) int) int {
	weights := make(chan int)
	var sum int
	var counted sync.WaitGroup
	counted.Add(len(nums))
	// Сумматор: складывает веса по мере поступления.
	go func() {
		for w := range weights {
			sum += w
			counted.Done()
		}
	}()

	for _, n := range nums {
		go func() {
			weights <- weight(n)
		}()
	}
	counted.Wait()
	return sum
}

// Every вызывает f раз в interval в отдельной горутине, пока не
// вызовут stop. После возврата stop f больше не вызывается.
func Every(interval time.Duration, f func()) (stop func()) {
	var mu sync.Mutex
	stopped := false
	go func() {
		t := time.NewTicker(interval)
		for range t.C {
			mu.Lock()
			if !stopped {
				f()
			}
			mu.Unlock()
		}
	}()
	return func() {
		mu.Lock()
		stopped = true
		mu.Unlock()
	}
}
