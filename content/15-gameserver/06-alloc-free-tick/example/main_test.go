package main

import (
	"bytes"
	"runtime"
	"runtime/debug"
	"sync"
	"testing"
)

// Оба тика дают побайтно одинаковые кадры — оптимизация не меняет
// протокол.
func TestSameFrames(t *testing.T) {
	a, b := NewWorld(200, 8), NewWorld(200, 8)
	for range 3 {
		a.TickNaive()
		b.TickFast()
	}
	for i := range a.Clients {
		if !bytes.Equal(a.Clients[i].Out, b.Clients[i].Out) {
			t.Fatalf("игрок %d: кадры различаются", i)
		}
	}
	t.Logf("кадр игрока 0: %d байт, первые байты % x", len(b.Clients[0].Out), b.Clients[0].Out[:6])
}

// `testing.AllocsPerRun` вызывает функцию один раз для прогрева
// и считает среднее число выделений на следующих вызовах. После
// прогрева буферы тика без аллокаций уже нужного размера.
func TestTickAllocs(t *testing.T) {
	w := NewWorld(200, 8)
	naive := testing.AllocsPerRun(100, w.TickNaive)
	fast := testing.AllocsPerRun(100, w.TickFast)
	t.Logf("наивный тик выделяет память: %v (больше одного выделения на игрока: %v)", naive > 0, naive > 8)
	t.Logf("тик без аллокаций: %v выделений", fast)
	if fast != 0 {
		t.Fatalf("TickFast: %v выделений на тик", fast)
	}
}

// Почему для буферов тика своя очередь свободных, а не только
// `sync.Pool`: пул очищается сборкой мусора. Объект переживает одну
// сборку в victim-кеше и пропадает после второй. Один P и выключенный
// автоматический GC — чтобы поведение пула было воспроизводимым.
func TestPoolDropsOnGC(t *testing.T) {
	defer runtime.GOMAXPROCS(runtime.GOMAXPROCS(1))
	defer debug.SetGCPercent(debug.SetGCPercent(-1))

	created := 0
	pool := sync.Pool{New: func() any { created++; return new([]byte) }}
	free := make(chan *[]byte, 4) // своя очередь свободных буферов

	buf := new([]byte)
	pool.Put(buf)
	free <- buf
	runtime.GC()
	runtime.GC()

	pool.Get()
	t.Logf("после двух сборок sync.Pool создал новый буфер: %v", created == 1)
	t.Logf("своя очередь вернула тот же буфер: %v", <-free == buf)
}

// Бенчмарки: в песочнице — кнопка «Бенчмарки» (-benchmem). Смотрите
// на B/op и allocs/op, а не только на ns/op: выделения — это работа
// сборщика мусора потом, во время других тиков.
func BenchmarkTickNaive(b *testing.B) {
	w := NewWorld(1000, 50)
	b.ReportAllocs()
	for b.Loop() {
		w.TickNaive()
	}
}

func BenchmarkTickFast(b *testing.B) {
	w := NewWorld(1000, 50)
	b.ReportAllocs()
	for b.Loop() {
		w.TickFast()
	}
}
