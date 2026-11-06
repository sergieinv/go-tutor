package task

// Sampler — буфер, который никогда не блокирует ни
// производителей, ни потребителей.
type Sampler struct {
	ch chan int
}

// NewSampler создаёт Sampler на size значений.
// size < 1 трактуется как 1.
func NewSampler(size int) *Sampler {
	return &Sampler{ch: make(chan int, max(size, 1))}
}

// Offer кладёт v в буфер. Если буфер полон, v отбрасывается
// и Offer возвращает false.
func (s *Sampler) Offer(v int) bool {
	select {
	case s.ch <- v:
		return true
	default:
		return false
	}
}

// TryTake забирает самое старое значение. Если буфер пуст,
// возвращает 0, false.
func (s *Sampler) TryTake() (int, bool) {
	select {
	case v := <-s.ch:
		return v, true
	default:
		return 0, false
	}
}

// Drain забирает значения от старых к новым — не больше,
// чем лежало в буфере в момент вызова, и останавливается
// раньше, если буфер опустел.
func (s *Sampler) Drain() []int {
	// len(s.ch) — только верхняя граница: без неё при непрерывном
	// потоке Offer цикл мог бы не закончиться никогда. Читаем без
	// ожидания: пока мы читаем, другой потребитель может забрать
	// значения, и блокирующее <-s.ch повисло бы.
	n := len(s.ch)
	var out []int
	for range n {
		v, ok := s.TryTake()
		if !ok {
			break
		}
		out = append(out, v)
	}
	return out
}
