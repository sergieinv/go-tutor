package task

import (
	"errors"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// callAsync запускает Call в горутине, чтобы тест не завис,
// если реализация ждёт f дольше таймаута.
func callAsync[T any](timeout time.Duration, f func() (T, error)) (T, error) {
	type res struct {
		v   T
		err error
	}
	ch := make(chan res, 1)
	go func() {
		v, err := Call(timeout, f)
		ch <- res{v, err}
	}()
	select {
	case r := <-ch:
		return r.v, r.err
	case <-time.After(3 * time.Second):
		panic("Call не вернулся по таймауту: он ждёт завершения f")
	}
}

func TestCallDoesNotWaitForF(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	_, err := callAsync(30*time.Millisecond, func() (string, error) {
		<-release
		return "late", nil
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("Call вернул ошибку %v, want ErrTimeout", err)
	}
}

func TestCallTimeoutReturnsZero(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	got, err := callAsync(30*time.Millisecond, func() ([]int, error) {
		<-release
		return []int{1}, nil
	})
	if !errors.Is(err, ErrTimeout) || got != nil {
		t.Fatalf("Call = (%v, %v), want (nil, ErrTimeout)", got, err)
	}
}

func TestCallNonPositiveTimeout(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		var calls atomic.Int32
		got, err := callAsync(d, func() (int, error) {
			calls.Add(1)
			return 1, nil
		})
		if !errors.Is(err, ErrTimeout) || got != 0 {
			t.Errorf("Call(%v) = (%d, %v), want (0, ErrTimeout)", d, got, err)
		}
		time.Sleep(20 * time.Millisecond)
		if n := calls.Load(); n != 0 {
			t.Errorf("Call(%v) вызвал f %d раз, want 0", d, n)
		}
	}
}

func TestCallCallsOnce(t *testing.T) {
	var calls atomic.Int32
	_, err := Call(time.Second, func() (int, error) {
		calls.Add(1)
		return 1, nil
	})
	if err != nil {
		t.Fatalf("Call вернул ошибку %v", err)
	}
	time.Sleep(20 * time.Millisecond)
	if n := calls.Load(); n != 1 {
		t.Errorf("f вызвана %d раз, want 1", n)
	}
}

func TestCallNoLeakAfterTimeout(t *testing.T) {
	before := runtime.NumGoroutine()
	release := make(chan struct{})
	for range 20 {
		_, err := callAsync(5*time.Millisecond, func() (int, error) {
			<-release
			return 1, nil
		})
		if !errors.Is(err, ErrTimeout) {
			t.Fatalf("Call вернул ошибку %v, want ErrTimeout", err)
		}
	}
	// f возвращаются уже после таймаута: их горутины
	// должны завершиться, а не повиснуть на отправке.
	close(release)
	deadline := time.Now().Add(2 * time.Second)
	for runtime.NumGoroutine() > before+2 && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before+2 {
		t.Fatalf("горутин было %d, стало %d: исполнители f не завершились после таймаута", before, n)
	}
}
