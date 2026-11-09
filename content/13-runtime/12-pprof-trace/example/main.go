// Программа сама снимает два профиля через `runtime/pprof`:
// CPU-профиль всей работы и профиль кучи в конце. В песочнице
// у неё кнопка «pprof»: runner запускает программу и показывает
// `go tool pprof -top` по каждому записанному файлу `*.pprof`.
// Числа в профиле от запуска к запуску разные, а вот stdout —
// детерминированный: программа печатает только результаты.
package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"runtime/pprof"
	"slices"
)

func main() {
	// **CPU-профиль.** `StartCPUProfile` включает таймер: 100 раз
	// в секунду процессорного времени поток получает сигнал
	// `SIGPROF`, и обработчик записывает стек текущей горутины.
	// Чтобы набрать выборку, программа должна поработать хотя бы
	// несколько десятых секунды.
	cpu, err := os.Create("cpu.pprof")
	if err != nil {
		fmt.Println("ошибка:", err)
		return
	}
	defer cpu.Close()
	if err := pprof.StartCPUProfile(cpu); err != nil {
		fmt.Println("ошибка:", err)
		return
	}

	// **Метки.** `pprof.Do` вешает на горутину метки: каждый
	// сэмпл CPU-профиля внутри `f` получит `phase=hash` или
	// `phase=sort`. В `go tool pprof` по ним фильтруют:
	// `-tagfocus=phase=sort`, а `-tags` показывает распределение.
	ctx := context.Background()
	var sum uint64
	pprof.Do(ctx, pprof.Labels("phase", "hash"), func(context.Context) {
		sum = hashRounds(25_000_000)
	})
	var median int
	pprof.Do(ctx, pprof.Labels("phase", "sort"), func(context.Context) {
		median = sortRounds(300)
	})
	pprof.StopCPUProfile()
	fmt.Printf("хеш: %x, медиана: %d\n", sum, median)

	// **Профиль кучи.** Он отражает состояние на момент последней
	// завершённой сборки мусора, поэтому перед записью зовём
	// `runtime.GC()`. Выборка — примерно один сэмпл на 512 КБ
	// выделенной памяти (`runtime.MemProfileRate`).
	index := buildIndex(200_000)
	runtime.GC()
	heap, err := os.Create("heap.pprof")
	if err != nil {
		fmt.Println("ошибка:", err)
		return
	}
	defer heap.Close()
	if err := pprof.Lookup("heap").WriteTo(heap, 0); err != nil {
		fmt.Println("ошибка:", err)
		return
	}
	fmt.Println("в индексе:", len(index))
	fmt.Println("записаны cpu.pprof и heap.pprof")
	runtime.KeepAlive(index)
}

// Горячая функция — её pprof покажет первой в `-top`.
func hashRounds(n int) uint64 {
	h := uint64(14695981039346656037) // FNV-1a
	for i := range n {
		for b := range 16 {
			h ^= uint64(i>>b) & 0xff
			h *= 1099511628211
		}
	}
	return h
}

// Вторая по весу: сортировка псевдослучайных чисел.
func sortRounds(rounds int) int {
	x := uint32(1)
	s := make([]int, 20_000)
	for range rounds {
		for i := range s {
			x ^= x << 13 // xorshift: детерминированный генератор
			x ^= x >> 17
			x ^= x << 5
			s[i] = int(x % 1_000_000)
		}
		slices.Sort(s)
	}
	return s[len(s)/2]
}

// Живые объекты в конце программы — их покажет `inuse_space`.
func buildIndex(n int) map[int][]byte {
	m := make(map[int][]byte, n)
	for i := range n {
		m[i] = make([]byte, 32)
	}
	return m
}
