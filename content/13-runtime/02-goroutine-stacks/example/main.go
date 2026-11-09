// Стек горутины начинается с нескольких килобайт и растёт
// по мере надобности: рантайм выделяет новый стек вдвое
// больше и копирует в него старый. Пример показывает рост,
// указатели в стек, адаптивный стартовый размер и что
// бывает, когда стек упирается в лимит.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"strings"
	"unsafe"
)

// Размер стека, с которым стартуют новые горутины.
func startingStack() uint64 {
	s := []metrics.Sample{{Name: "/gc/stack/starting-size:bytes"}}
	metrics.Read(s)
	return s[0].Value.Uint64()
}

// Обычная рекурсия: каждый вызов — новый кадр на стеке.
func depth(n int) int {
	if n == 0 {
		return 0
	}
	return depth(n-1) + 1
}

// p указывает на переменную в стеке вызывающей функции.
// Пока рекурсия растёт, стек несколько раз переезжает,
// а p в каждом кадре рантайм исправляет на новый адрес.
func grow(p *int, n int) {
	if n == 0 {
		*p++
		return
	}
	grow(p, n-1)
}

func main() {
	if mode := os.Getenv("STACK_DEMO"); mode != "" {
		child(mode)
		return
	}

	// Без единой сборки мусора стартовый стек — минимальный:
	// 2 КБ в Linux и macOS (в Windows — 8 КБ).
	fmt.Println("стартовый стек горутины:", startingStack(), "байт")

	// Миллион вложенных вызовов — десятки мегабайт стека.
	// Поток C# с его 1 МБ упал бы с StackOverflowException,
	// а горутина просто вырастет.
	done := make(chan int)
	go func() { done <- depth(1_000_000) }()
	fmt.Println("рекурсия глубиной 1000000, результат:", <-done)

	// `x` не убегает в кучу: компилятор доказал, что указатель
	// на неё не переживёт кадр. Поэтому при копировании стека
	// рантайм знает все указатели на `x` и исправляет их.
	x := 41
	before := uintptr(unsafe.Pointer(&x))
	grow(&x, 100_000)
	fmt.Println("адрес x изменился после роста стека:", uintptr(unsafe.Pointer(&x)) != before)
	fmt.Println("x через указатель:", x)

	// Сборщик мусора считает средний размер стеков, которые
	// просканировал, и новые горутины стартуют с таким стеком.
	// Одна «глубокая» горутина во время GC поднимает его для всех.
	hold, release := make(chan struct{}), make(chan struct{})
	go func() {
		var deep func(n int)
		deep = func(n int) {
			var pad [64]byte
			_ = pad
			if n == 0 {
				close(hold)
				<-release
				return
			}
			deep(n - 1)
		}
		deep(50_000)
	}()
	<-hold
	runtime.GC()
	fmt.Println("после GC с глубокой горутиной стартовый стек вырос:", startingStack() > 2048)
	close(release)

	// Переполнение стека — не паника, а fatal error: `recover`
	// не поможет, процесс завершается с кодом 2. Покажем
	// в дочернем процессе с лимитом стека 1 000 000 байт.
	out, stderr, code := run("overflow", "")
	for _, line := range strings.Split(stderr, "\n") {
		if strings.HasPrefix(line, "runtime: goroutine stack exceeds") || strings.HasPrefix(line, "fatal error") {
			fmt.Println("переполнение:", line)
		}
	}
	fmt.Printf("код выхода: %d, recover сработал: %v\n", code, strings.Contains(out, "recover"))

	// А обычная паника печатает стеки по правилу GOTRACEBACK:
	// single — только упавшая горутина, all — все пользовательские.
	for _, tb := range []string{"single", "all"} {
		_, stderr, _ := run("panic", tb)
		fmt.Printf("паника, GOTRACEBACK=%s: горутин в отчёте %d\n", tb, strings.Count(stderr, "\ngoroutine "))
	}
}

// child — то, что выполняет дочерний процесс.
func child(mode string) {
	if mode == "overflow" {
		debug.SetMaxStack(1_000_000)
		defer func() {
			if r := recover(); r != nil {
				fmt.Println("recover:", r)
			}
		}()
		var inf func(n int) int
		inf = func(n int) int { return inf(n+1) + 1 }
		inf(0)
	}
	// Две горутины ждут вечно, main паникует.
	for range 2 {
		go func() { select {} }()
	}
	runtime.Gosched()
	panic("что-то пошло не так")
}

// run перезапускает этот же бинарник в режиме mode.
func run(mode, traceback string) (stdout, stderr string, code int) {
	exe, err := os.Executable()
	if err != nil {
		panic(err)
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), "STACK_DEMO="+mode)
	if traceback != "" {
		cmd.Env = append(cmd.Env, "GOTRACEBACK="+traceback)
	}
	var o, e strings.Builder
	cmd.Stdout, cmd.Stderr = &o, &e
	cmd.Run()
	return o.String(), e.String(), cmd.ProcessState.ExitCode()
}
