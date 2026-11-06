package task

// Sampler — буфер, который никогда не блокирует ни
// производителей, ни потребителей.
type Sampler struct {
	// TODO: поля
}

// NewSampler создаёт Sampler на size значений.
// size < 1 трактуется как 1.
func NewSampler(size int) *Sampler {
	// TODO: реализуйте функцию
	return &Sampler{}
}

// Offer кладёт v в буфер. Если буфер полон, v отбрасывается
// и Offer возвращает false.
func (s *Sampler) Offer(v int) bool {
	// TODO: реализуйте метод
	return false
}

// TryTake забирает самое старое значение. Если буфер пуст,
// возвращает 0, false.
func (s *Sampler) TryTake() (int, bool) {
	// TODO: реализуйте метод
	return 0, false
}

// Drain забирает значения от старых к новым — не больше,
// чем лежало в буфере в момент вызова, и останавливается
// раньше, если буфер опустел.
func (s *Sampler) Drain() []int {
	// TODO: реализуйте метод
	return nil
}
