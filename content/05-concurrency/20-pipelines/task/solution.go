package task

import (
	"context"
	"sync"
)

// Merge сливает каналы chans в один (fan-in). Выход закрывается,
// когда все входы закрыты, или после отмены ctx. nil-каналы
// пропускаются.
func Merge[T any](ctx context.Context, chans ...<-chan T) <-chan T {
	out := make(chan T)
	var wg sync.WaitGroup
	for _, c := range chans {
		if c == nil {
			// Из nil-канала ничего не придёт, и он никогда
			// не закроется — горутина на него не нужна.
			continue
		}
		wg.Go(func() {
			for {
				// Чтение тоже ждёт ctx.Done(): вход может
				// никогда не закрыться.
				var v T
				var ok bool
				select {
				case v, ok = <-c:
					if !ok {
						return
					}
				case <-ctx.Done():
					return
				}
				// Отправка — через select: читатель мог уйти.
				select {
				case out <- v:
				case <-ctx.Done():
					return
				}
			}
		})
	}
	// Закрывает выход единственный владелец — горутина,
	// дождавшаяся всех пересылающих.
	go func() {
		wg.Wait()
		close(out)
	}()
	return out
}
