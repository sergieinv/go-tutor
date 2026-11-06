package task

import (
	"context"
	"sync"
	"time"
)

// Lookup вызывает get для всех ключей одновременно и собирает
// ответы в map. Если ctx отменён раньше, чем пришли все ответы,
// возвращает nil и ctx.Err(), не дожидаясь get.
func Lookup(ctx context.Context, keys []string, get func(string) string) (map[string]string, error) {
	type pair struct{ key, value string }
	// Утечка 1: после отмены ctx из replies никто не читает, и
	// небуферизованная отправка вешала опоздавшие горутины навсегда.
	// Буфер на все ответы — отправка никогда не блокируется.
	replies := make(chan pair, len(keys))
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
	// Утечка 2: weights никто не закрывал, и сумматор вечно ждал
	// в range. Все веса уже получены — отправителей больше нет,
	// закрывать безопасно.
	close(weights)
	return sum
}

// Every вызывает f раз в interval в отдельной горутине, пока не
// вызовут stop. После возврата stop f больше не вызывается.
func Every(interval time.Duration, f func()) (stop func()) {
	// Утечка 3: цикл по t.C не имел выхода — флаг лишь глушил
	// вызовы f, а горутина и тикер жили вечно. Теперь stop
	// закрывает done, горутина выходит и останавливает тикер.
	done := make(chan struct{})
	exited := make(chan struct{})
	go func() {
		defer close(exited)
		t := time.NewTicker(interval)
		defer t.Stop()
		for {
			select {
			case <-done:
				return
			case <-t.C:
				f()
			}
		}
	}()

	var once sync.Once
	return func() {
		// Повторный close паникует — закрываем один раз.
		once.Do(func() { close(done) })
		// Ждём выхода горутины: после этого f точно не вызывается.
		<-exited
	}
}
