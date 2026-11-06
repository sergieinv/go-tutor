package task

import (
	"slices"
	"testing"
	"time"
)

func TestDebounceFlushOnClose(t *testing.T) {
	in := make(chan string)
	// Огромная пауза: если реализация ждёт d после закрытия in,
	// тест упадёт по лимиту collect.
	out := Debounce(in, time.Hour)
	in <- "a"
	in <- "b"
	close(in)
	if got := collect(t, out, 2*time.Second); !slices.Equal(got, []string{"b"}) {
		t.Fatalf("получено %v, want [b]: при закрытии in ожидающее значение отдаётся сразу", got)
	}
}

func TestDebounceCloseWithoutPending(t *testing.T) {
	in := make(chan string)
	out := Debounce(in, time.Hour)
	close(in)
	if got := collect(t, out, 2*time.Second); len(got) != 0 {
		t.Fatalf("получено %v, want ничего", got)
	}
}

func TestDebounceNothingAfterEmit(t *testing.T) {
	in := make(chan string)
	out := Debounce(in, 30*time.Millisecond)
	go func() {
		in <- "a"
		// После выдачи "a" ждать нечего: при закрытии
		// не должно появиться повторного "a".
		time.Sleep(300 * time.Millisecond)
		close(in)
	}()
	if got := collect(t, out, 3*time.Second); !slices.Equal(got, []string{"a"}) {
		t.Fatalf("получено %v, want [a]", got)
	}
}

func TestDebounceRestartsOnEachValue(t *testing.T) {
	const d = 300 * time.Millisecond
	in := make(chan string)
	out := Debounce(in, d)
	lastSent := make(chan time.Time, 1)
	go func() {
		in <- "a"
		time.Sleep(50 * time.Millisecond)
		in <- "b"
		time.Sleep(50 * time.Millisecond)
		start := time.Now()
		in <- "c"
		lastSent <- start
		time.Sleep(d + 300*time.Millisecond)
		close(in)
	}()

	select {
	case v := <-out:
		if v != "c" {
			t.Fatalf("первое значение %q, want \"c\": пауза должна отсчитываться от последнего значения", v)
		}
		if waited := time.Since(<-lastSent); waited < d {
			t.Fatalf("значение выдано через %v после последнего, want не раньше %v", waited, d)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("значение не выдано")
	}
	if got := collect(t, out, 3*time.Second); len(got) != 0 {
		t.Fatalf("после \"c\" получено %v, want ничего", got)
	}
}
