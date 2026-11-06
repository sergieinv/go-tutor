package task

// SumAll вызывает каждый источник в своей горутине и возвращает
// сумму результатов, переданных через канал.
func SumAll(sources []func() int) int {
	results := make(chan int)
	for _, src := range sources {
		go func() {
			results <- src() // ждёт, пока SumAll заберёт значение
		}()
	}
	// Получаем ровно столько значений, сколько горутин запустили:
	// иначе часть отправителей застрянет навсегда.
	sum := 0
	for range sources {
		sum += <-results
	}
	return sum
}
