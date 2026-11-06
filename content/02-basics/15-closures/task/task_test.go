package task

import "testing"

func TestCounter(t *testing.T) {
	c := Counter(1, 2)
	for _, want := range []int{1, 3, 5, 7} {
		if got := c(); got != want {
			t.Fatalf("Counter(1, 2)() = %d, want %d", got, want)
		}
	}
}

func TestMemo(t *testing.T) {
	square := func(n int) int { return n * n }
	call, calls := Memo(square)
	if got := call(4); got != 16 {
		t.Errorf("call(4) = %d, want 16", got)
	}
	call(4)
	if got := calls(); got != 1 {
		t.Errorf("после двух call(4) calls() = %d, want 1", got)
	}
}
