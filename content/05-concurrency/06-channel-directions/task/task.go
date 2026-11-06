package task

// Range отправляет from, from+1, …, to-1 и закрывает канал.
func Range(from, to int) <-chan int {
	// TODO: реализуйте функцию
	return nil
}

// Filter пересылает значения, для которых keep вернула true,
// и закрывает свой канал после закрытия in.
func Filter(in <-chan int, keep func(int) bool) <-chan int {
	// TODO: реализуйте функцию
	return nil
}

// Sum читает in до закрытия и возвращает сумму.
func Sum(in <-chan int) int {
	// TODO: реализуйте функцию
	return 0
}
