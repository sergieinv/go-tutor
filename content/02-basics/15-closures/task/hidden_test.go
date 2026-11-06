package task

import "testing"

func TestCounterIndependent(t *testing.T) {
	a := Counter(0, 1)
	b := Counter(100, -10)
	a()
	a()
	if got := a(); got != 2 {
		t.Errorf("третий вызов Counter(0, 1) = %d, want 2", got)
	}
	if got := b(); got != 100 {
		t.Errorf("первый вызов Counter(100, -10) = %d, want 100 — счётчики должны быть независимы", got)
	}
	if got := b(); got != 90 {
		t.Errorf("второй вызов Counter(100, -10) = %d, want 90", got)
	}
}

func TestCounterZeroStep(t *testing.T) {
	c := Counter(5, 0)
	for range 3 {
		if got := c(); got != 5 {
			t.Fatalf("Counter(5, 0)() = %d, want 5", got)
		}
	}
}

func TestMemoCountsDistinct(t *testing.T) {
	real := 0
	f := func(n int) int {
		real++
		return n + 1
	}
	call, calls := Memo(f)
	for _, n := range []int{1, 2, 1, 3, 2, 1, 0, 0} {
		if got := call(n); got != n+1 {
			t.Errorf("call(%d) = %d, want %d", n, got, n+1)
		}
	}
	if real != 4 || calls() != 4 {
		t.Errorf("f вызвана %d раз, calls() = %d, want 4 и 4", real, calls())
	}
}

func TestMemoZeroResultCached(t *testing.T) {
	// Результат 0 тоже должен кэшироваться — проверка через ok, а не через != 0.
	call, calls := Memo(func(int) int { return 0 })
	call(7)
	call(7)
	if got := calls(); got != 1 {
		t.Errorf("нулевой результат не закэширован: calls() = %d, want 1", got)
	}
}

func TestMemoIndependent(t *testing.T) {
	id := func(n int) int { return n }
	call1, calls1 := Memo(id)
	_, calls2 := Memo(id)
	call1(1)
	call1(2)
	if calls1() != 2 || calls2() != 0 {
		t.Errorf("кэши разных Memo должны быть независимы: %d, %d", calls1(), calls2())
	}
}
