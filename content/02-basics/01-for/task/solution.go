package task

// Primes возвращает все простые числа, меньшие n, по возрастанию.
// Для n <= 2 возвращает пустой срез.
func Primes(n int) []int {
	primes := []int{}
candidates:
	for i := 2; i < n; i++ {
		// Делители достаточно проверять до корня из i.
		for d := 2; d*d <= i; d++ {
			if i%d == 0 {
				continue candidates
			}
		}
		primes = append(primes, i)
	}
	return primes
}

// CollatzSteps возвращает число шагов, за которое последовательность
// Коллатца доходит от n до 1. Для n <= 0 возвращает -1.
func CollatzSteps(n int) int {
	if n <= 0 {
		return -1
	}
	steps := 0
	for n != 1 {
		if n%2 == 0 {
			n /= 2
		} else {
			n = 3*n + 1
		}
		steps++
	}
	return steps
}
