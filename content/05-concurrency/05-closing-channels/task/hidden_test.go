package task

import (
	"runtime"
	"slices"
	"testing"
	"time"
)

func TestBatchEmpty(t *testing.T) {
	if got := collect(t, Batch(feed(), 3)); len(got) != 0 {
		t.Errorf("Batch(пустой поток) = %v, want ни одной пачки", got)
	}
}

func TestBatchNonPositiveSize(t *testing.T) {
	for _, size := range []int{0, -2} {
		got := collect(t, Batch(feed(1, 2, 3), size))
		want := [][]int{{1}, {2}, {3}}
		if !slices.EqualFunc(got, want, slices.Equal) {
			t.Errorf("Batch(size=%d) = %v, want %v", size, got, want)
		}
	}
}

// Получатель меняет пачку — другие пачки не должны измениться.
func TestBatchNoAliasing(t *testing.T) {
	out := Batch(feed(1, 2, 3, 4, 5, 6, 7), 3)
	first := <-out
	first[0], first[1], first[2] = -1, -1, -1
	second := <-out
	third := <-out
	if _, ok := <-out; ok {
		t.Fatal("лишняя пачка")
	}
	if !slices.Equal(second, []int{4, 5, 6}) || !slices.Equal(third, []int{7}) {
		t.Errorf("пачки после изменения первой: %v %v; want [4 5 6] [7]", second, third)
	}
	second[0] = 100
	if third[0] != 7 {
		t.Errorf("пачки делят память: изменение второй задело третью")
	}
}

// Batch должен вернуть канал, не дожидаясь закрытия in.
func TestBatchStreams(t *testing.T) {
	in := make(chan int)
	ret := make(chan chan []int, 1)
	go func() { ret <- Batch(in, 2) }()
	var out chan []int
	select {
	case out = <-ret:
	case <-time.After(time.Second):
		close(in)
		t.Fatal("Batch не вернулся, пока in открыт: обработка должна идти в горутине")
	}
	in <- 1
	in <- 2
	select {
	case b := <-out:
		if !slices.Equal(b, []int{1, 2}) {
			t.Errorf("первая пачка = %v, want [1 2]", b)
		}
	case <-time.After(time.Second):
		t.Fatal("полная пачка не отправлена, пока in открыт")
	}
	in <- 3
	close(in)
	if got := collect(t, out); !slices.EqualFunc(got, [][]int{{3}}, slices.Equal) {
		t.Errorf("хвост = %v, want [[3]]", got)
	}
}

func TestBatchNoLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 10 {
		collect(t, Batch(feed(1, 2, 3, 4, 5), 2))
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("после закрытия выходных каналов осталось %d лишних горутин", n-before)
	}
}
