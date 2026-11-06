package task

import (
	"errors"
	"testing"
	"time"
)

func closed() <-chan string {
	ch := make(chan string)
	close(ch)
	return ch
}

func TestRaceClosedSourceIsSkipped(t *testing.T) {
	got, err := Race(closed(), send("b", 30*time.Millisecond), time.Second)
	if err != nil || got != "b" {
		t.Fatalf("Race(closed, b) = (%q, %v), want (\"b\", nil)", got, err)
	}
}

func TestRaceBothClosed(t *testing.T) {
	start := time.Now()
	_, err := Race(closed(), closed(), time.Second)
	if !errors.Is(err, ErrNoResult) {
		t.Fatalf("Race(closed, closed) вернул ошибку %v, want ErrNoResult", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Errorf("Race(closed, closed) ждал %v, а должен вернуться сразу", d)
	}
}

func TestRaceNilChannel(t *testing.T) {
	got, err := Race(nil, send("b", 10*time.Millisecond), time.Second)
	if err != nil || got != "b" {
		t.Fatalf("Race(nil, b) = (%q, %v), want (\"b\", nil)", got, err)
	}
}

func TestRaceTimeoutIsTotal(t *testing.T) {
	a := make(chan string)
	go func() {
		time.Sleep(60 * time.Millisecond)
		close(a)
	}()
	never := make(chan string)

	start := time.Now()
	_, err := Race(a, never, 100*time.Millisecond)
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Race вернул ошибку %v, want ErrTimeout", err)
	}
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Errorf("Race ждал %v при timeout 100ms: таймаут перезапускается после закрытия канала", d)
	}
}
