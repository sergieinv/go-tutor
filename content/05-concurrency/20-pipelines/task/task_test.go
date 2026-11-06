package task

import (
	"context"
	"slices"
	"testing"
	"time"
)

// from возвращает закрытый канал, в буфере которого лежат vals.
func from(vals ...int) <-chan int {
	ch := make(chan int, len(vals))
	for _, v := range vals {
		ch <- v
	}
	close(ch)
	return ch
}

// collect читает out до закрытия; если канал не закрылся
// за d, тест падает.
func collect(t *testing.T, out <-chan int, d time.Duration) []int {
	t.Helper()
	timer := time.NewTimer(d)
	defer timer.Stop()
	var got []int
	for {
		select {
		case v, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, v)
		case <-timer.C:
			t.Fatalf("выходной канал не закрылся за %v, получено %v", d, got)
			return nil
		}
	}
}

func TestMergeAll(t *testing.T) {
	out := Merge(context.Background(), from(1, 2, 3), from(10, 20), from(100))
	got := collect(t, out, 2*time.Second)
	slices.Sort(got)
	want := []int{1, 2, 3, 10, 20, 100}
	if !slices.Equal(got, want) {
		t.Fatalf("Merge отдал %v, want (в любом порядке) %v", got, want)
	}
}

func TestMergeKeepsOrderWithinInput(t *testing.T) {
	a, b := make([]int, 100), make([]int, 100)
	for i := range 100 {
		a[i], b[i] = i, 1000+i
	}
	got := collect(t, Merge(context.Background(), from(a...), from(b...)), 2*time.Second)
	if len(got) != 200 {
		t.Fatalf("получено %d значений, want 200", len(got))
	}
	lastA, lastB := -1, 999
	for _, v := range got {
		if v < 1000 {
			if v < lastA {
				t.Fatalf("значения из одного входа перепутаны: %d после %d", v, lastA)
			}
			lastA = v
		} else {
			if v < lastB {
				t.Fatalf("значения из одного входа перепутаны: %d после %d", v, lastB)
			}
			lastB = v
		}
	}
}

func TestMergeNoInputs(t *testing.T) {
	got := collect(t, Merge[int](context.Background()), time.Second)
	if len(got) != 0 {
		t.Fatalf("Merge() отдал %v, want ничего", got)
	}
}
