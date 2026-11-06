package task

import (
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

func TestRunLimitedNonPositiveLimit(t *testing.T) {
	for _, limit := range []int{0, -3} {
		p, tasks := newProbe(4, 1)
		RunLimited(tasks, limit)
		p.check(t, 4, 1, 1)
	}
}

func TestRunLimitedEmpty(t *testing.T) {
	start := time.Now()
	RunLimited(nil, 3)
	RunLimited([]func(){}, 0)
	if d := time.Since(start); d > time.Second {
		t.Errorf("RunLimited без задач работал %v", d)
	}
}

func TestRunLimitedEachOnce(t *testing.T) {
	counts := make([]atomic.Int32, 50)
	tasks := make([]func(), len(counts))
	for i := range tasks {
		tasks[i] = func() { counts[i].Add(1) }
	}
	RunLimited(tasks, 7)
	for i := range counts {
		if c := counts[i].Load(); c != 1 {
			t.Fatalf("задача %d вызвана %d раз, want 1", i, c)
		}
	}
}

func TestRunLimitedManyTasksNoLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	p, tasks := newProbe(200, 8)
	RunLimited(tasks, 8)
	p.check(t, 200, 8, 8)
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("после RunLimited осталось %d лишних горутин", n-before)
	}
}
