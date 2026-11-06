package task

// Chunk делит s на последовательные части длиной n.
// Последняя часть может быть короче. Части ссылаются
// на массив s без копирования, но append к любой части
// не должен портить соседние. При n <= 0 — panic.
func Chunk(s []int, n int) [][]int {
	if n <= 0 {
		panic("Chunk: n must be positive")
	}
	chunks := make([][]int, 0, (len(s)+n-1)/n)
	for len(s) > 0 {
		end := min(n, len(s))
		// Третий индекс обрезает ёмкость: append к части
		// выделит новый массив вместо записи в соседнюю.
		chunks = append(chunks, s[:end:end])
		s = s[end:]
	}
	return chunks
}
