package task

import (
	"errors"
	"testing"
	"time"
)

func TestCallReturnsResult(t *testing.T) {
	got, err := Call(time.Second, func() (int, error) { return 42, nil })
	if err != nil || got != 42 {
		t.Fatalf("Call = (%d, %v), want (42, nil)", got, err)
	}
}

func TestCallPassesError(t *testing.T) {
	boom := errors.New("boom")
	got, err := Call(time.Second, func() (string, error) { return "partial", boom })
	if !errors.Is(err, boom) || got != "partial" {
		t.Fatalf("Call = (%q, %v), want (\"partial\", boom)", got, err)
	}
}

func TestCallTimeout(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	got, err := Call(50*time.Millisecond, func() (int, error) {
		<-release
		return 7, nil
	})
	if !errors.Is(err, ErrTimeout) || got != 0 {
		t.Fatalf("Call = (%d, %v), want (0, ErrTimeout)", got, err)
	}
}
