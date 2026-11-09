package main

import (
	"sync"
	"sync/atomic"
	"testing"
	"unsafe"
)

// Размеры не зависят от машины: `unsafe.Sizeof` считает
// компилятор. Шаг между соседними счётчиками — 8 байт против
// одной линии.
func TestLayout(t *testing.T) {
	var p Packed
	var q Padded
	t.Logf("Packed: %d байт, шаг %d", unsafe.Sizeof(p), unsafe.Sizeof(p[0]))
	t.Logf("Padded: %d байт, шаг %d", unsafe.Sizeof(q), unsafe.Sizeof(q[0]))
	if unsafe.Sizeof(q[0]) < CacheLine {
		t.Fatal("соседние счётчики Padded могут делить линию")
	}
}

// False sharing — проблема скорости, а не корректности:
// атомарные счётчики считают верно в обоих вариантах.
func TestConcurrentInc(t *testing.T) {
	var p Packed
	var q Padded
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for range 10_000 {
				p.Inc(g)
				q.Inc(g)
			}
		})
	}
	wg.Wait()
	t.Logf("Packed: %d, Padded: %d", p.Sum(), q.Sum())
	if p.Sum() != 80_000 || q.Sum() != 80_000 {
		t.Fatal("потеряны инкременты")
	}
}

// `RunParallel` запускает GOMAXPROCS горутин; каждая берёт
// свой номер слота. Запустите локально с `-cpu 1,4,8`: при
// одном ядре варианты почти равны, при нескольких Packed и
// Shared отстают — линия ездит между ядрами.
func bench(b *testing.B, inc func(slot int)) {
	var next atomic.Int64
	b.RunParallel(func(pb *testing.PB) {
		slot := int(next.Add(1) - 1)
		for pb.Next() {
			inc(slot)
		}
	})
}

// Настоящий общий счётчик (true sharing) — для сравнения.
func BenchmarkShared(b *testing.B) {
	var n atomic.Int64
	bench(b, func(int) { n.Add(1) })
}

func BenchmarkPacked(b *testing.B) {
	var p Packed
	bench(b, p.Inc)
}

func BenchmarkPadded(b *testing.B) {
	var p Padded
	bench(b, p.Inc)
}

// Читатели `RWMutex` не ждут друг друга, но каждый `RLock` и
// `RUnlock` — атомарная запись в общий `readerCount`: короткие
// чтения упираются в ту же линию, что и `Mutex`.
var table = map[int]int{1: 10, 2: 20}

func BenchmarkMutexRead(b *testing.B) {
	var mu sync.Mutex
	bench(b, func(int) {
		mu.Lock()
		_ = table[1]
		mu.Unlock()
	})
}

func BenchmarkRWMutexRead(b *testing.B) {
	var mu sync.RWMutex
	bench(b, func(int) {
		mu.RLock()
		_ = table[1]
		mu.RUnlock()
	})
}
