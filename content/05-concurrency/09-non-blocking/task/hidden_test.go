package task

import (
	"slices"
	"sync"
	"testing"
	"time"
)

// within падает, если f не закончилась за 3 секунды:
// ни один метод Sampler не должен блокироваться.
func within(t *testing.T, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("операция заблокировалась")
	}
}

func TestSamplerSizeBelowOne(t *testing.T) {
	for _, size := range []int{0, -5} {
		s := NewSampler(size)
		var first, second bool
		within(t, func() {
			first = s.Offer(1)
			second = s.Offer(2)
		})
		if !first || second {
			t.Errorf("NewSampler(%d): Offer = %v, %v; want true, false (размер 1)", size, first, second)
		}
	}
}

func TestSamplerReuseAfterDrain(t *testing.T) {
	s := NewSampler(2)
	s.Offer(1)
	s.Offer(2)
	s.Drain()
	if !s.Offer(3) || !s.Offer(4) || s.Offer(5) {
		t.Fatal("после Drain буфер должен снова принимать size значений")
	}
	if got := s.Drain(); !slices.Equal(got, []int{3, 4}) {
		t.Fatalf("Drain() = %v, want [3 4]", got)
	}
}

func TestSamplerConcurrent(t *testing.T) {
	const producers, perProducer = 8, 2000
	s := NewSampler(16)

	var mu sync.Mutex
	accepted, taken := 0, 0

	within(t, func() {
		var prod, cons sync.WaitGroup
		stop := make(chan struct{})

		// Потребители читают и через Drain, и через TryTake
		// одновременно: Drain не должен зависнуть, если значения
		// у него из-под носа забрал соседний потребитель.
		for i := range 4 {
			cons.Add(1)
			go func() {
				defer cons.Done()
				for {
					n := 0
					if i%2 == 0 {
						n = len(s.Drain())
					} else if _, ok := s.TryTake(); ok {
						n = 1
					}
					mu.Lock()
					taken += n
					mu.Unlock()
					select {
					case <-stop:
						return
					default:
					}
				}
			}()
		}
		for range producers {
			prod.Add(1)
			go func() {
				defer prod.Done()
				ok := 0
				for v := range perProducer {
					if s.Offer(v) {
						ok++
					}
				}
				mu.Lock()
				accepted += ok
				mu.Unlock()
			}()
		}
		prod.Wait()
		close(stop)
		cons.Wait()
	})

	rest := len(s.Drain())
	if taken+rest != accepted {
		t.Fatalf("принято %d значений, а забрано %d: значения теряются или дублируются", accepted, taken+rest)
	}
}

// Производители непрерывно подкладывают значения: Drain всё равно
// должен вернуться и забрать не больше size значений.
func TestSamplerDrainUnderConstantOffer(t *testing.T) {
	const size = 64
	s := NewSampler(size)
	stop := make(chan struct{})
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
					s.Offer(1)
				}
			}
		})
	}
	defer func() {
		close(stop)
		wg.Wait()
	}()
	for range 100 {
		var got []int
		within(t, func() { got = s.Drain() })
		if len(got) > size {
			t.Fatalf("Drain вернул %d значений, want не больше %d (ёмкость буфера)", len(got), size)
		}
	}
}
