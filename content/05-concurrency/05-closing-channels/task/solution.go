package task

// Batch читает числа из in и отправляет их пачками по size штук
// в возвращаемый канал. Когда in закрыт и все пачки отправлены,
// выходной канал закрывается.
func Batch(in chan int, size int) chan []int {
	size = max(size, 1)
	out := make(chan []int)
	go func() {
		defer close(out) // отправитель закрывает свой канал
		var batch []int
		for v := range in { // до закрытия in
			batch = append(batch, v)
			if len(batch) == size {
				out <- batch
				batch = nil // новая пачка — новый срез, а не batch[:0]
			}
		}
		if len(batch) > 0 {
			out <- batch // хвост
		}
	}()
	return out
}
