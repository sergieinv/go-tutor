package task

import "time"

// Debounce пропускает значение из in, только если после него
// d не приходило новых. Когда in закрыт, ожидающее значение
// отдаётся сразу, а возвращённый канал закрывается.
func Debounce(in <-chan string, d time.Duration) <-chan string {
	out := make(chan string)
	go func() {
		defer close(out)

		// Таймер создаём остановленным: пока значений нет,
		// ждать нечего. С Go 1.23 после Stop в timer.C
		// не останется устаревшего значения, и Reset безопасен.
		timer := time.NewTimer(d)
		timer.Stop()

		var pending string
		has := false
		for {
			select {
			case v, ok := <-in:
				if !ok {
					if has {
						out <- pending
					}
					return
				}
				// Новое значение заменяет ожидающее и
				// перезапускает отсчёт тишины.
				pending, has = v, true
				timer.Reset(d)
			case <-timer.C:
				out <- pending
				has = false
			}
		}
	}()
	return out
}
