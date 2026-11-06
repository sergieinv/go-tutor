// Pipeline с узким местом и утечкой — образец для тестов построителя goroutines.
package main

import (
	"fmt"
	"time"
)

func gen(n int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for i := range n {
			out <- i
		}
	}()
	return out
}

func square(in <-chan int) <-chan int {
	out := make(chan int)
	go func() {
		defer close(out)
		for v := range in {
			time.Sleep(time.Millisecond)
			out <- v * v
		}
	}()
	return out
}

func main() {
	sum := 0
	for v := range square(gen(8)) {
		sum += v
	}
	fmt.Println(sum)
	// Утечка: никто не читает результат.
	for range 3 {
		go func() {
			ch := make(chan int)
			ch <- 1
		}()
	}
	time.Sleep(5 * time.Millisecond)
}
