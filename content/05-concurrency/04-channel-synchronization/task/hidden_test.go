package task

import (
	"fmt"
	"runtime"
	"testing"
	"time"
)

func TestAlternateGoroutines(t *testing.T) {
	j := newJournal()
	Alternate(5, j.a, j.b)
	if len(j.aIDs) != 1 || len(j.bIDs) != 1 {
		t.Fatalf("a вызывалась из %d горутин, b — из %d; want по одной", len(j.aIDs), len(j.bIDs))
	}
	self := goid()
	for id := range j.aIDs {
		if j.bIDs[id] {
			t.Errorf("a и b вызывались из одной горутины")
		}
		if id == self {
			t.Errorf("a вызывалась из горутины, вызвавшей Alternate")
		}
	}
	for id := range j.bIDs {
		if id == self {
			t.Errorf("b вызывалась из горутины, вызвавшей Alternate")
		}
	}
}

func TestAlternateLongGame(t *testing.T) {
	j := newJournal()
	const n = 200
	Alternate(n, j.a, j.b)
	if len(j.log) != 2*n {
		t.Fatalf("вызовов %d, want %d", len(j.log), 2*n)
	}
	for i := range n {
		if j.log[2*i] != fmt.Sprint("a", i) || j.log[2*i+1] != fmt.Sprint("b", i) {
			t.Fatalf("на ходу %d: %v %v, want a%d b%d", i, j.log[2*i], j.log[2*i+1], i, i)
		}
	}
}

func TestAlternateReturnsAfterLastB(t *testing.T) {
	finished := false
	b := func(i int) {
		if i == 2 {
			time.Sleep(30 * time.Millisecond)
			finished = true // гонка, если Alternate не дождался b
		}
	}
	Alternate(3, func(int) {}, b)
	if !finished {
		t.Error("Alternate вернулся раньше, чем завершился последний вызов b")
	}
}

func TestAlternateZero(t *testing.T) {
	for _, n := range []int{0, -1} {
		called := false
		f := func(int) { called = true }
		doneCh := make(chan struct{})
		go func() {
			Alternate(n, f, f)
			close(doneCh)
		}()
		select {
		case <-doneCh:
		case <-time.After(time.Second):
			t.Fatalf("Alternate(%d, …) не вернулся", n)
		}
		if called {
			t.Errorf("Alternate(%d, …) вызвал a или b", n)
		}
	}
}

func TestAlternateNoLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 10 {
		Alternate(4, func(int) {}, func(int) {})
	}
	Alternate(0, func(int) {}, func(int) {})
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("после Alternate осталось %d лишних горутин", n-before)
	}
}
