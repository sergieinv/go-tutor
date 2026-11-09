package main

import (
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
)

// Чтобы поведение пула было предсказуемым, тест фиксирует один P
// (у каждого P своя часть пула) и выключает автоматический GC:
// сборки идут только там, где тест зовёт `runtime.GC()`.
func quiet(t *testing.T) {
	procs := runtime.GOMAXPROCS(1)
	gc := debug.SetGCPercent(-1)
	t.Cleanup(func() {
		runtime.GOMAXPROCS(procs)
		debug.SetGCPercent(gc)
	})
}

func TestFormat(t *testing.T) {
	a := FormatNew("ann", []int{3, 14, 159})
	b := FormatPooled("ann", []int{3, 14, 159})
	if a != "ann: 3,14,159" || a != b {
		t.Fatalf("FormatNew = %q, FormatPooled = %q", a, b)
	}
	// Буфер вернулся в пул грязным, но Reset его очистил.
	if c := FormatPooled("bob", nil); c != "bob: " {
		t.Fatalf("FormatPooled = %q, want %q", c, "bob: ")
	}
}

// `testing.AllocsPerRun` — среднее число аллокаций на вызов.
func TestAllocs(t *testing.T) {
	quiet(t)
	items := []int{1, 22, 333, 4444, 55555, 666666, 7777777}
	withoutPool := testing.AllocsPerRun(1000, func() { FormatNew("user-42", items) })
	withPool := testing.AllocsPerRun(1000, func() { FormatPooled("user-42", items) })
	t.Logf("аллокаций на вызов: без пула %.0f, с пулом %.0f", withoutPool, withPool)
	if withPool != 1 || withoutPool <= withPool {
		t.Fatalf("без пула %.0f, с пулом %.0f", withoutPool, withPool)
	}
}

// Сборка мусора не очищает пул сразу: основной кеш переезжает
// в victim-кеш и живёт до следующей сборки.
func TestVictimCache(t *testing.T) {
	quiet(t)

	var p sync.Pool
	x := new(int)
	p.Put(x)
	runtime.GC()
	got, _ := p.Get().(*int)
	t.Logf("после одной сборки получили тот же объект: %v", got == x)
	if got != x {
		t.Fatal("объект не пережил одну сборку")
	}

	p.Put(x)
	runtime.GC()
	runtime.GC()
	empty := p.Get() == nil
	t.Logf("после двух сборок пул пуст: %v", empty)
	if !empty {
		t.Fatal("объект пережил две сборки")
	}
}

// Срез в интерфейсе — аллокация на каждый Put; указатель — нет.
func TestPutSliceVsPointer(t *testing.T) {
	quiet(t)

	var sp sync.Pool
	backing := make([]byte, 0, 512)
	byValue := testing.AllocsPerRun(100, func() {
		b, ok := sp.Get().([]byte)
		if !ok {
			b = backing
		}
		sp.Put(b[:0]) // упаковка заголовка среза в any
	})
	byPointer := testing.AllocsPerRun(100, func() {
		bp := slicePool.Get().(*[]byte)
		*bp = (*bp)[:0]
		slicePool.Put(bp)
	})
	t.Logf("аллокаций на цикл Get/Put: []byte — %.0f, *[]byte — %.0f", byValue, byPointer)
	if byValue != 1 || byPointer != 0 {
		t.Fatalf("Put([]byte) = %.0f, Put(*[]byte) = %.0f", byValue, byPointer)
	}
}

// Бенчмарк: сравните B/op и allocs/op у двух вариантов.
func BenchmarkFormat(b *testing.B) {
	items := []int{1, 22, 333, 4444, 55555, 666666, 7777777}
	b.Run("new", func(b *testing.B) {
		for b.Loop() {
			FormatNew("user-42", items)
		}
	})
	b.Run("pool", func(b *testing.B) {
		for b.Loop() {
			FormatPooled("user-42", items)
		}
	})
	// Параллельно: у каждого P своя часть пула, блокировок нет.
	b.Run("pool-parallel", func(b *testing.B) {
		b.RunParallel(func(pb *testing.PB) {
			for pb.Next() {
				FormatPooled("user-42", items)
			}
		})
	})
}
