package task

import (
	"go/parser"
	"go/token"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
)

// Стек без блокировок: пакет sync (Mutex, RWMutex…) в решении
// не используется, только sync/atomic.
func TestHiddenNoLocks(t *testing.T) {
	f, err := parser.ParseFile(token.NewFileSet(), "task.go", nil, parser.ImportsOnly)
	if err != nil {
		t.Fatalf("не удалось разобрать task.go: %v", err)
	}
	for _, imp := range f.Imports {
		if p, _ := strconv.Unquote(imp.Path.Value); p == "sync" {
			t.Fatal("task.go импортирует sync: стек должен обходиться без блокировок")
		}
	}
}

func TestHiddenZeroValues(t *testing.T) {
	var s Stack[*int]
	if v, ok := s.Pop(); ok || v != nil {
		t.Fatalf("Pop() на пустом = %v, %v", v, ok)
	}
	s.PushMany()
	s.PushMany([]*int(nil)...)
	if got := s.Drain(); got != nil {
		t.Fatalf("после пустых PushMany Drain() = %v, want nil", got)
	}
	x := 5
	s.Push(&x)
	s.Push(nil) // nil — обычное значение
	if v, ok := s.Pop(); !ok || v != nil {
		t.Fatalf("Pop() = %v, %v; want nil, true", v, ok)
	}
	if v, ok := s.Pop(); !ok || v != &x {
		t.Fatalf("Pop() = %v, %v; want &x, true", v, ok)
	}
}

func TestHiddenPushManyOrder(t *testing.T) {
	var s Stack[int]
	vs := []int{1, 2, 3}
	s.PushMany(vs...)
	vs[0] = 100 // стек хранит копии значений
	s.PushMany(4)
	for _, want := range []int{4, 3, 2, 1} {
		if v, ok := s.Pop(); !ok || v != want {
			t.Fatalf("Pop() = %d, %v; want %d", v, ok, want)
		}
	}
	s.Push(7)
	if got := s.Drain(); !slices.Equal(got, []int{7}) {
		t.Fatalf("Drain() = %v, want [7]", got)
	}
	if _, ok := s.Pop(); ok {
		t.Fatal("после Drain стек не пуст")
	}
}

// Каждый Drain, идущий одновременно с PushMany, забирает пачки
// только целиком: подряд и от последнего элемента к первому.
func TestHiddenPushManyAtomic(t *testing.T) {
	const writers, batches, size = 4, 150, 4
	var s Stack[int]
	var mu sync.Mutex // только для сбора результатов в тесте
	var drained [][]int
	var done atomic.Bool

	var drainers sync.WaitGroup
	for range 2 {
		drainers.Go(func() {
			for {
				stop := done.Load()
				if d := s.Drain(); d != nil {
					mu.Lock()
					drained = append(drained, d)
					mu.Unlock()
				}
				if stop {
					return
				}
			}
		})
	}
	var wg sync.WaitGroup
	for w := range writers {
		wg.Go(func() {
			for b := range batches {
				base := (w*batches + b) * size
				s.PushMany(base, base+1, base+2, base+3)
			}
		})
	}
	wg.Wait()
	done.Store(true)
	drainers.Wait()
	if rest := s.Drain(); rest != nil {
		drained = append(drained, rest)
	}

	seen := make([]bool, writers*batches*size)
	for _, d := range drained {
		if len(d)%size != 0 {
			t.Fatalf("Drain вернул %d значений — пачка разорвана: %v", len(d), d)
		}
		for i := 0; i < len(d); i += size {
			base := d[i] - (size - 1)
			if base%size != 0 {
				t.Fatalf("пачка начинается не с последнего элемента: %v", d[i:i+size])
			}
			for j := range size {
				v := d[i+j]
				if v != base+size-1-j {
					t.Fatalf("пачка перемешана: %v", d[i:i+size])
				}
				if seen[v] {
					t.Fatalf("значение %d вытолкнуто дважды", v)
				}
				seen[v] = true
			}
		}
	}
	for v, ok := range seen {
		if !ok {
			t.Fatalf("значение %d потеряно", v)
		}
	}
}

// Смешанная нагрузка: Push, PushMany, Pop и Drain одновременно.
// Каждое значение выходит ровно один раз.
func TestHiddenStressExactlyOnce(t *testing.T) {
	const producers, per = 4, 600
	total := producers * per
	var s Stack[int]
	counts := make([]atomic.Int32, total)

	take := func(v int) {
		if v < 0 || v >= total {
			t.Errorf("чужое значение %d", v)
			return
		}
		counts[v].Add(1)
	}
	// Потребители выходят, когда производители закончили и
	// стек опустел, — даже если решение теряет значения.
	var done atomic.Bool
	var consumers sync.WaitGroup
	for c := range 4 {
		consumers.Go(func() {
			for {
				stop := done.Load()
				took := false
				if c == 0 {
					for _, v := range s.Drain() {
						take(v)
						took = true
					}
				} else if v, ok := s.Pop(); ok {
					take(v)
					took = true
				}
				if stop && !took {
					return
				}
			}
		})
	}
	var wg sync.WaitGroup
	for p := range producers {
		wg.Go(func() {
			for i := 0; i < per; i += 3 {
				base := p*per + i
				if i%2 == 0 {
					s.PushMany(base, base+1, base+2)
				} else {
					s.Push(base)
					s.Push(base + 1)
					s.Push(base + 2)
				}
			}
		})
	}
	wg.Wait()
	done.Store(true)
	consumers.Wait()
	for _, v := range s.Drain() {
		take(v)
	}
	for v := range counts {
		if n := counts[v].Load(); n != 1 {
			t.Fatalf("значение %d вытолкнуто %d раз", v, n)
		}
	}
}
