package task

// Range отправляет from, from+1, …, to-1 и закрывает канал.
func Range(from, to int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := from; i < to; i++ {
			out <- i
		}
	}()
	return out // chan int неявно сужается до <-chan int
}

// Filter пересылает значения, для которых keep вернула true,
// и закрывает свой канал после закрытия in.
func Filter(in <-chan int, keep func(int) bool) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			if keep(v) {
				out <- v
			}
		}
	}()
	return out
}

// Sum читает in до закрытия и возвращает сумму.
func Sum(in <-chan int) int {
	total := 0
	for v := range in {
		total += v
	}
	return total
}
