package task

import (
	"slices"
	"testing"
	"time"
)

// collect читает out до закрытия; падает, если это заняло
// больше limit.
func collect(t *testing.T, out <-chan string, limit time.Duration) []string {
	t.Helper()
	if out == nil {
		t.Fatal("Debounce вернул nil-канал")
	}
	var got []string
	timeout := time.After(limit)
	for {
		select {
		case v, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, v)
		case <-timeout:
			t.Fatalf("канал не закрылся за %v, получено %v", limit, got)
		}
	}
}

func TestDebounceBurst(t *testing.T) {
	in := make(chan string)
	out := Debounce(in, 50*time.Millisecond)
	go func() {
		for _, v := range []string{"a", "b", "c"} {
			in <- v
		}
		time.Sleep(300 * time.Millisecond)
		close(in)
	}()
	if got := collect(t, out, 3*time.Second); !slices.Equal(got, []string{"c"}) {
		t.Fatalf("получено %v, want [c]", got)
	}
}

func TestDebounceTwoBursts(t *testing.T) {
	in := make(chan string)
	out := Debounce(in, 50*time.Millisecond)
	go func() {
		in <- "x1"
		in <- "x2"
		time.Sleep(300 * time.Millisecond)
		in <- "y1"
		in <- "y2"
		time.Sleep(300 * time.Millisecond)
		close(in)
	}()
	if got := collect(t, out, 3*time.Second); !slices.Equal(got, []string{"x2", "y2"}) {
		t.Fatalf("получено %v, want [x2 y2]", got)
	}
}
