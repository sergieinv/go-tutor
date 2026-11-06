package task

import (
	"errors"
	"slices"
	"sync"
	"testing"
	"time"
)

func TestWaitTimeoutDone(t *testing.T) {
	var wg sync.WaitGroup
	for range 3 {
		wg.Go(func() { time.Sleep(5 * time.Millisecond) })
	}
	if !WaitTimeout(&wg, 5*time.Second) {
		t.Fatal("WaitTimeout = false, want true: все горутины завершились задолго до таймаута")
	}
}

func TestWaitTimeoutExpires(t *testing.T) {
	var wg sync.WaitGroup
	wg.Add(1)
	defer wg.Done()
	if WaitTimeout(&wg, 20*time.Millisecond) {
		t.Fatal("WaitTimeout = true, want false: счётчик не дошёл до нуля")
	}
}

func TestRunAllErrorsByIndex(t *testing.T) {
	e1, e3 := errors.New("e1"), errors.New("e3")
	errs, ok := RunAll(5*time.Second,
		func() error { return nil },
		func() error { time.Sleep(10 * time.Millisecond); return e1 },
		func() error { return nil },
		func() error { return e3 },
	)
	if !ok {
		t.Fatal("RunAll вернул ok = false, want true")
	}
	if want := []error{nil, e1, nil, e3}; !slices.Equal(errs, want) {
		t.Fatalf("RunAll = %v, want %v", errs, want)
	}
}
