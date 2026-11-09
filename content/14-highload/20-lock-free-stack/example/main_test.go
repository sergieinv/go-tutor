package main

import (
	"sync"
	"testing"
)

func TestLIFO(t *testing.T) {
	var s Stack
	for i := 1; i <= 3; i++ {
		s.Push(i)
	}
	var got []int
	for {
		v, ok := s.Pop()
		if !ok {
			break
		}
		got = append(got, v)
	}
	t.Logf("вытолкнуто: %v", got)
	if len(got) != 3 || got[0] != 3 || got[2] != 1 {
		t.Fatalf("got %v, want [3 2 1]", got)
	}
}

// Стресс-тест под `-race`: проверяем корректность, а не скорость.
// Четыре горутины кладут непересекающиеся числа, четыре
// выталкивают, пока не соберут всё. Каждое число должно
// встретиться ровно один раз.
func TestConcurrent(t *testing.T) {
	const producers, perProducer = 4, 2000
	const total = producers * perProducer
	var s Stack
	seen := make([]int, total)
	var mu sync.Mutex
	var popped sync.WaitGroup
	var count int

	var wg sync.WaitGroup
	for p := range producers {
		wg.Go(func() {
			for i := range perProducer {
				s.Push(p*perProducer + i)
			}
		})
	}
	for range 4 {
		popped.Go(func() {
			for {
				mu.Lock()
				done := count == total
				mu.Unlock()
				if done {
					return
				}
				if v, ok := s.Pop(); ok {
					mu.Lock()
					seen[v]++
					count++
					mu.Unlock()
				}
			}
		})
	}
	wg.Wait()
	popped.Wait()

	bad := 0
	for _, n := range seen {
		if n != 1 {
			bad++
		}
	}
	t.Logf("вытолкнуто %d из %d, потеряно или повторено: %d", count, total, bad)
	if bad != 0 {
		t.Fatal("значения потеряны или продублированы")
	}
}

// Бенчмарки: пары Push/Pop из GOMAXPROCS горутин. Сравните
// `-cpu 1,4,8` и `ns/op` вместе с `allocs/op`: lock-free стек
// выделяет узел на каждый Push, а при конкуренции его CAS
// повторяются.
type stack interface {
	Push(int)
	Pop() (int, bool)
}

func benchStack(b *testing.B, s stack) {
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			s.Push(1)
			s.Pop()
		}
	})
}

func BenchmarkLockFree(b *testing.B) { benchStack(b, &Stack{}) }
func BenchmarkMutex(b *testing.B)    { benchStack(b, &MutexStack{}) }
