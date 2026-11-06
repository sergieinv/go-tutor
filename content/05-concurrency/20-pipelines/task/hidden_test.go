package task

import (
	"context"
	"runtime"
	"testing"
	"time"
)

// waitGoroutines ждёт, пока число горутин опустится до base.
func waitGoroutines(base int, d time.Duration) int {
	deadline := time.Now().Add(d)
	for {
		n := runtime.NumGoroutine()
		if n <= base || time.Now().After(deadline) {
			return n
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestMergeSkipsNil(t *testing.T) {
	got := collect(t, Merge(context.Background(), nil, from(7), nil), time.Second)
	if len(got) != 1 || got[0] != 7 {
		t.Fatalf("Merge(nil, [7], nil) отдал %v, want [7]", got)
	}
	got = collect(t, Merge[int](context.Background(), nil), time.Second)
	if len(got) != 0 {
		t.Fatalf("Merge(nil) отдал %v, want ничего", got)
	}
}

func TestMergeDoesNotBlock(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	silent := make(chan int) // никто не пишет и не закрывает
	done := make(chan struct{})
	go func() {
		Merge(ctx, silent, silent)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Merge заблокировался: он должен сразу вернуть канал, а пересылать — в горутинах")
	}
}

func TestMergeCancelWithoutReader(t *testing.T) {
	base := runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	silent := make(chan int)                 // вход, который никогда не закроется
	out := Merge(ctx, silent, from(1, 2, 3)) // значения есть, но выход никто не читает
	time.Sleep(20 * time.Millisecond)
	cancel()

	if n := waitGoroutines(base, 2*time.Second); n > base {
		t.Fatalf("после отмены ctx осталось %d лишних горутин: пересылающие горутины должны выходить по ctx.Done(), даже если выход никто не читает", n-base)
	}
	// Выход закрыт; значения, отправленные до отмены, могли потеряться.
	collect(t, out, time.Second)
}

func TestMergeCancelStopsReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in := make(chan int)
	out := Merge(ctx, in)
	select {
	case in <- 1:
	case <-time.After(time.Second):
		t.Fatal("Merge не читает входной канал")
	}
	select {
	case v := <-out:
		if v != 1 {
			t.Fatalf("получено %d, want 1", v)
		}
	case <-time.After(time.Second):
		t.Fatal("значение из входа не пришло в выход")
	}
	cancel()
	// Вход не закрыт, но выход обязан закрыться.
	collect(t, out, time.Second)
}

func TestMergeManyProducers(t *testing.T) {
	const inputs, per = 8, 500
	chans := make([]<-chan int, inputs)
	for i := range inputs {
		ch := make(chan int)
		chans[i] = ch
		go func() {
			defer close(ch)
			for j := range per {
				ch <- j
			}
		}()
	}
	got := collect(t, Merge(context.Background(), chans...), 5*time.Second)
	if len(got) != inputs*per {
		t.Fatalf("получено %d значений, want %d", len(got), inputs*per)
	}
	sum := 0
	for _, v := range got {
		sum += v
	}
	if want := inputs * per * (per - 1) / 2; sum != want {
		t.Fatalf("сумма %d, want %d", sum, want)
	}
}
