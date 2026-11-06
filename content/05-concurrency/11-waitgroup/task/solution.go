package task

import (
	"sync"
	"time"
)

// WaitTimeout ждёт, пока счётчик wg дойдёт до нуля, но не дольше d.
// Возвращает true, если дождалась, и false по таймауту.
func WaitTimeout(wg *sync.WaitGroup, d time.Duration) bool {
	// Сам wg.Wait прервать нельзя, поэтому ждём его в помощнике,
	// а здесь выбираем между его сигналом и таймером.
	done := make(chan struct{})
	go func() {
		wg.Wait()
		// close, а не отправка: отправка в небуферизованный канал
		// после таймаута повисла бы навсегда — получателя уже нет.
		close(done)
	}()

	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-done:
		return true
	case <-t.C:
		return false
	}
}

// RunAll запускает каждую задачу в своей горутине и ждёт их не
// дольше d. Если все успели — ошибки по индексам и true, иначе nil и false.
func RunAll(d time.Duration, tasks ...func() error) ([]error, bool) {
	errs := make([]error, len(tasks))
	var wg sync.WaitGroup
	for i, task := range tasks {
		// wg.Go делает Add до запуска горутины и Done по её возврату.
		// Каждая горутина пишет только свой errs[i] — мьютекс не нужен.
		wg.Go(func() {
			errs[i] = task()
		})
	}
	if !WaitTimeout(&wg, d) {
		// Задачи ещё пишут в errs: отдать срез наружу — гонка.
		return nil, false
	}
	// Wait (внутри WaitTimeout) вернулся — все записи в errs видны.
	return errs, true
}
