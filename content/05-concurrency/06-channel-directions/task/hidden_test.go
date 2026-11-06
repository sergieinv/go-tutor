package task

import (
	"runtime"
	"testing"
	"time"
)

func TestRangeEmpty(t *testing.T) {
	for _, r := range [][2]int{{5, 5}, {5, 1}} {
		if got := drain(t, Range(r[0], r[1])); len(got) != 0 {
			t.Errorf("Range(%d, %d) = %v, want без значений", r[0], r[1], got)
		}
	}
}

func TestRangeNegative(t *testing.T) {
	if got := Sum(Range(-3, 2)); got != -5 {
		t.Errorf("Sum(Range(-3, 2)) = %d, want -5", got)
	}
}

func TestSumEmpty(t *testing.T) {
	ch := make(chan int)
	close(ch)
	if got := Sum(ch); got != 0 {
		t.Errorf("Sum(закрытый пустой) = %d, want 0", got)
	}
}

func TestFilterNothingKept(t *testing.T) {
	if got := drain(t, Filter(Range(0, 5), func(int) bool { return false })); len(got) != 0 {
		t.Errorf("Filter без подходящих = %v, want пусто", got)
	}
}

func TestFilterKeepCalledOnce(t *testing.T) {
	calls := map[int]int{}
	// keep вызывается только из горутины Filter, и Sum дожидается
	// закрытия её канала, — поэтому map без мьютекса безопасна.
	Sum(Filter(Range(0, 20), func(v int) bool { calls[v]++; return true }))
	for v := range 20 {
		if calls[v] != 1 {
			t.Fatalf("keep(%d) вызвана %d раз, want 1", v, calls[v])
		}
	}
}

// Стадии должны возвращать канал сразу и работать в потоке.
func TestStagesStream(t *testing.T) {
	in := make(chan int)
	ret := make(chan (<-chan int), 1)
	go func() { ret <- Filter(in, func(int) bool { return true }) }()
	var out <-chan int
	select {
	case out = <-ret:
	case <-time.After(time.Second):
		close(in)
		t.Fatal("Filter не вернулся, пока in открыт: пересылка должна идти в горутине")
	}
	in <- 42
	select {
	case v := <-out:
		if v != 42 {
			t.Errorf("получено %d, want 42", v)
		}
	case <-time.After(time.Second):
		t.Fatal("значение не переслано, пока in открыт")
	}
	close(in)
	if got := drain(t, out); len(got) != 0 {
		t.Errorf("лишние значения %v", got)
	}

	done := make(chan (<-chan int), 1)
	go func() { done <- Range(0, 1000) }()
	select {
	case r := <-done:
		if got := drain(t, r); len(got) != 1000 {
			t.Errorf("Range(0, 1000) дал %d значений, want 1000", len(got))
		}
	case <-time.After(time.Second):
		t.Fatal("Range не вернулся сразу: отправка должна идти в горутине")
	}
}

func TestPipelineNoLeaks(t *testing.T) {
	before := runtime.NumGoroutine()
	for range 10 {
		Sum(Filter(Range(0, 50), func(v int) bool { return v%3 == 0 }))
	}
	deadline := time.Now().Add(time.Second)
	for runtime.NumGoroutine() > before && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if n := runtime.NumGoroutine(); n > before {
		t.Errorf("после Sum осталось %d лишних горутин", n-before)
	}
}
