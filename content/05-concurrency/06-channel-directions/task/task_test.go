package task

import (
	"slices"
	"testing"
	"time"
)

// drain читает канал до закрытия, но не дольше секунды.
func drain(t *testing.T, in <-chan int) []int {
	t.Helper()
	if in == nil {
		t.Fatal("получен nil-канал")
	}
	var got []int
	timeout := time.After(time.Second)
	for {
		select {
		case v, ok := <-in:
			if !ok {
				return got
			}
			got = append(got, v)
		case <-timeout:
			t.Fatalf("канал не закрыт; получено %v", got)
		}
	}
}

func even(v int) bool { return v%2 == 0 }

func TestRange(t *testing.T) {
	if got := drain(t, Range(3, 7)); !slices.Equal(got, []int{3, 4, 5, 6}) {
		t.Errorf("Range(3, 7) = %v, want [3 4 5 6]", got)
	}
}

func TestFilter(t *testing.T) {
	if got := drain(t, Filter(Range(0, 10), even)); !slices.Equal(got, []int{0, 2, 4, 6, 8}) {
		t.Errorf("Filter(Range(0, 10), even) = %v, want [0 2 4 6 8]", got)
	}
}

func TestPipeline(t *testing.T) {
	if got := Sum(Filter(Range(1, 11), even)); got != 30 {
		t.Errorf("Sum(Filter(Range(1, 11), even)) = %d, want 30", got)
	}
}
