package task

import (
	"errors"
	"testing"
	"time"
)

func send(v string, after time.Duration) <-chan string {
	ch := make(chan string, 1)
	go func() {
		time.Sleep(after)
		ch <- v
	}()
	return ch
}

func TestRaceFirstWins(t *testing.T) {
	got, err := Race(send("a", 10*time.Millisecond), send("b", 200*time.Millisecond), time.Second)
	if err != nil || got != "a" {
		t.Fatalf("Race = (%q, %v), want (\"a\", nil)", got, err)
	}
	got, err = Race(send("a", 200*time.Millisecond), send("b", 10*time.Millisecond), time.Second)
	if err != nil || got != "b" {
		t.Fatalf("Race = (%q, %v), want (\"b\", nil)", got, err)
	}
}

func TestRaceTimeout(t *testing.T) {
	never := make(chan string)
	_, err := Race(never, never, 50*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Race без значений вернул ошибку %v, want ErrTimeout", err)
	}
}
