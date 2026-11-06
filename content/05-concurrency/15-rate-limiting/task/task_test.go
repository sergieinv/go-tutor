package task

import (
	"testing"
	"time"
)

func TestAllowBurst(t *testing.T) {
	// Пополнение раз в час: в тесте работает только начальный запас.
	l := NewLimiter(time.Hour, 3)
	defer l.Stop()
	for i := 1; i <= 3; i++ {
		if !l.Allow() {
			t.Fatalf("Allow #%d = false, want true: ведро создаётся полным", i)
		}
	}
	if l.Allow() {
		t.Fatal("Allow #4 = true, want false: токены кончились")
	}
}

func TestWaitRate(t *testing.T) {
	const (
		interval = 20 * time.Millisecond
		burst    = 2
		n        = 6
	)
	start := time.Now()
	l := NewLimiter(interval, burst)
	defer l.Stop()
	for range n {
		l.Wait()
	}
	// За время d ведро выдаёт не больше burst + d/interval токенов.
	if d, min := time.Since(start), (n-burst)*interval; d < min {
		t.Errorf("%d вызовов Wait заняли %v, want не меньше %v", n, d, min)
	}
}
