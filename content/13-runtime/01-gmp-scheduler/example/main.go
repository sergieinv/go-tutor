// Планировщик не виден из кода, но его решения можно
// наблюдать: сколько горутин выполняется одновременно,
// вытесняется ли горутина без вызовов функций и сколько
// потоков ОС стоит ожидание сети и системного вызова.
// Флаг урока `GODEBUG=schedtrace=100` печатает состояние
// планировщика в stderr каждые 100 мс — разбор строки в тексте.

package main

import (
	"fmt"
	"os"
	"runtime"
	"runtime/metrics"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// `runtime/metrics` читает счётчики рантайма без остановки
// мира. Число живых потоков ОС — `/sched/threads/total`
// (метрика есть с Go 1.26).
func metric(name string) uint64 {
	s := []metrics.Sample{{Name: name}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

// Ждём, пока счётчик дорастёт до n, — так программа узнаёт,
// что горутины действительно заблокировались.
func waitFor(name string, n uint64) {
	deadline := time.Now().Add(5 * time.Second)
	for metric(name) < n && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
}

// Простые числа в [lo, hi) — просто работа для процессора.
func primes(lo, hi int) int {
	n := 0
	for x := max(lo, 2); x < hi; x++ {
		p := true
		for d := 2; d*d <= x; d++ {
			if x%d == 0 {
				p = false
				break
			}
		}
		if p {
			n++
		}
	}
	return n
}

func main() {

	// Флаг урока `runner.gomaxprocs: 2` задаёт переменную
	// окружения `GOMAXPROCS`: у планировщика два P, Go-код
	// одновременно выполняют не больше двух потоков.
	fmt.Println("GOMAXPROCS:", runtime.GOMAXPROCS(0))

	// Восемь вычислительных горутин на два P: шесть ждут
	// в очередях. В schedtrace это видно по `runqueue=`
	// и счётчикам локальных очередей `[ … ]`.
	counts := make([]int, 8)
	var wg sync.WaitGroup
	for i := range counts {
		wg.Go(func() { counts[i] = primes(i*1_000_000, (i+1)*1_000_000) })
	}
	wg.Wait()
	total := 0
	for _, c := range counts {
		total += c
	}
	fmt.Println("простых чисел до 8 000 000:", total)

	// Асинхронное вытеснение. На одном P горутина крутит цикл
	// без вызовов функций. `main` засыпает на 30 мс, а
	// проснуться может, только если P освободится. Через 10 мс
	// sysmon шлёт потоку сигнал и вытесняет цикл.
	prev := runtime.GOMAXPROCS(1)
	var stop atomic.Bool
	done := make(chan struct{})
	go func() {
		for !stop.Load() {
		}
		close(done)
	}()
	time.Sleep(30 * time.Millisecond)
	stop.Store(true)
	<-done
	fmt.Println("main получил P, хотя цикл не вызывал функций")
	runtime.GOMAXPROCS(prev)

	// 20 горутин ждут данных из `os.Pipe`. Файл переведён
	// в неблокирующий режим, и ожидание идёт через netpoller:
	// горутина спит, поток свободен.
	const n = 20
	base := metric("/sched/threads/total:threads")
	var files []*os.File
	for range n {
		r, w, err := os.Pipe()
		if err != nil {
			panic(err)
		}
		files = append(files, r, w)
		wg.Go(func() { r.Read(make([]byte, 1)) })
	}
	waitFor("/sched/goroutines/waiting:goroutines", n)
	netThreads := metric("/sched/threads/total:threads")
	for i := 1; i < len(files); i += 2 {
		files[i].Write([]byte{1})
	}
	wg.Wait()
	for _, f := range files {
		f.Close()
	}
	fmt.Println("netpoller: потоков почти не прибавилось:", netThreads-base < 5)

	// Те же 20 ожиданий, но через блокирующий `read(2)`:
	// `syscall.Read` на обычном (блокирующем) дескрипторе.
	// Поток уходит в ядро вместе с горутиной, P у него
	// забирают (handoff), и каждому ожиданию нужен свой поток.
	var fds [][2]int
	for range n {
		var p [2]int
		if err := syscall.Pipe(p[:]); err != nil {
			panic(err)
		}
		fds = append(fds, p)
		wg.Go(func() { syscall.Read(p[0], make([]byte, 1)) })
	}
	waitFor("/sched/goroutines/not-in-go:goroutines", n)
	sysThreads := metric("/sched/threads/total:threads")
	for _, p := range fds {
		syscall.Write(p[1], []byte{1})
	}
	wg.Wait()
	for _, p := range fds {
		syscall.Close(p[0])
		syscall.Close(p[1])
	}
	fmt.Println("syscall: потоков не меньше, чем ожиданий:", sysThreads >= n)
}
