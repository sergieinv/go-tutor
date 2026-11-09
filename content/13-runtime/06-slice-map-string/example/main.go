// Срез, строка и map — это маленькие заголовки, за которыми
// стоят данные в куче. Пример заглядывает внутрь: размеры
// заголовков, рост `append` по формуле рантайма, копирование
// при конвертации `string`↔`[]byte` и память map после `clear`.

package main

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"unsafe"
)

// Глобальные переменные-приёмники: запись в них уводит данные
// в кучу, и компилятор не может выбросить или «упростить» код.
var (
	sinkInts  []int
	sinkStr   string
	sinkBytes []byte
	sinkN     int
)

// growth дописывает n элементов и запоминает каждую новую ёмкость.
// Срез уходит в `sinkInts` на каждой итерации, поэтому массив
// сразу живёт в куче и растёт по чистой формуле `growslice`.
func growth(n int) []int {
	var s, caps []int
	for i := range n {
		s = append(s, i)
		sinkInts = s
		if len(caps) == 0 || caps[len(caps)-1] != cap(s) {
			caps = append(caps, cap(s))
		}
	}
	return caps
}

// heap возвращает объём живой кучи сразу после полной сборки.
func heap() uint64 {
	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	return ms.HeapAlloc
}

func main() {
	// Заголовок среза — три слова (ptr, len, cap), строки — два
	// (ptr, len), а map — один указатель на структуру рантайма.
	fmt.Println("sizeof []int, string, map:",
		unsafe.Sizeof([]int{}), unsafe.Sizeof(""), unsafe.Sizeof(map[int]int{}))

	// Подстрока не копирует байты: её указатель смотрит внутрь
	// исходной строки. `unsafe.StringData` достаёт этот указатель.
	str := "hello, gopher"
	sub := str[7:]
	inside := unsafe.Pointer(unsafe.StringData(sub)) ==
		unsafe.Add(unsafe.Pointer(unsafe.StringData(str)), 7)
	fmt.Println("substring shares bytes:", inside)

	// Рост `append`: до 256 элементов ёмкость удваивается, дальше
	// растёт плавно к 1.25x, и байты всегда округляются вверх
	// до класса размера аллокатора — отсюда 848, а не 832.
	fmt.Println("caps:", growth(2000))

	// Пять элементов в пустой срез: формула просит 5 (40 байт),
	// ближайший класс размера — 48 байт, то есть 6 элементов.
	five := append([]int(nil), 1, 2, 3, 4, 5)
	sinkInts = five
	fmt.Println("append 5 to nil: len", len(five), "cap", cap(five))

	// `string(b)` копирует байты: строка неизменяема, а срез
	// можно поменять. Но там, где копия не может «утечь», компилятор
	// её убирает: ключ map, сравнение, обход `range []byte(s)`.
	b := []byte("the key is longer than 32 bytes!!")
	m := map[string]int{string(b): 1}
	fmt.Println("allocs string(b):", testing.AllocsPerRun(100, func() { sinkStr = string(b) }))
	fmt.Println("allocs m[string(b)]:", testing.AllocsPerRun(100, func() { sinkN = m[string(b)] }))
	fmt.Println("allocs string(b) == s:", testing.AllocsPerRun(100, func() {
		if string(b) == str {
			sinkN++
		}
	}))
	fmt.Println("allocs []byte(s):", testing.AllocsPerRun(100, func() { sinkBytes = []byte(str) }))

	// `+=` в цикле создаёт новую строку на каждой итерации.
	// `strings.Builder` с `Grow` пишет в один буфер и отдаёт
	// его как строку без копии: одна аллокация на всё.
	parts := []string{"go", "runtime", "slices", "maps", "strings"}
	fmt.Println("allocs +=:", testing.AllocsPerRun(100, func() {
		r := ""
		for _, p := range parts {
			r += p + " "
		}
		sinkStr = r
	}))
	fmt.Println("allocs Builder:", testing.AllocsPerRun(100, func() {
		var sb strings.Builder
		sb.Grow(64)
		for _, p := range parts {
			sb.WriteString(p)
			sb.WriteByte(' ')
		}
		sinkStr = sb.String()
	}))

	// `clear` удаляет элементы, но не возвращает память: таблицы
	// map остаются того же размера. Освобождает её только сборщик,
	// когда на старую map больше никто не ссылается.
	big := make(map[int]int)
	for i := range 200_000 {
		big[i] = i
	}
	full := heap()
	clear(big)
	afterClear := heap()
	runtime.KeepAlive(big)
	big = make(map[int]int)
	afterNew := heap()
	runtime.KeepAlive(big)
	fmt.Println("len after clear:", len(big))
	fmt.Println("memory kept after clear:", afterClear > full*9/10)
	fmt.Println("memory freed with new map:", afterNew < full/10)

	// Порядок обхода map рантайм нарочно делает случайным:
	// каждый `range` начинает со случайной группы и слота.
	small := map[int]bool{}
	for i := range 20 {
		small[i] = true
	}
	first := order(small)
	differs := false
	for range 50 {
		if order(small) != first {
			differs = true
		}
	}
	fmt.Println("iteration order changes:", differs)
}

// order склеивает ключи в порядке обхода.
func order(m map[int]bool) string {
	var sb strings.Builder
	for k := range m {
		fmt.Fprint(&sb, k, ",")
	}
	return sb.String()
}
