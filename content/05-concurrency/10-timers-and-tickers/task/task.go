package task

import "time"

// Debounce пропускает значение из in, только если после него
// d не приходило новых. Когда in закрыт, ожидающее значение
// отдаётся сразу, а возвращённый канал закрывается.
func Debounce(in <-chan string, d time.Duration) <-chan string {
	// TODO: реализуйте функцию
	return nil
}
