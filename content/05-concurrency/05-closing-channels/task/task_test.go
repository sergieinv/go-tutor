package task

import (
	"slices"
	"testing"
	"time"
)

// feed отправляет значения в новый канал и закрывает его.
func feed(values ...int) chan int {
	ch := make(chan int)
	go func() {
		defer close(ch)
		for _, v := range values {
			ch <- v
		}
	}()
	return ch
}

// collect читает пачки до закрытия канала, но не дольше секунды.
func collect(t *testing.T, out chan []int) [][]int {
	t.Helper()
	if out == nil {
		t.Fatal("Batch вернул nil-канал")
	}
	var got [][]int
	timeout := time.After(time.Second)
	for {
		select {
		case b, ok := <-out:
			if !ok {
				return got
			}
			got = append(got, b)
		case <-timeout:
			t.Fatalf("выходной канал не закрыт; получено %v", got)
		}
	}
}

func TestBatch(t *testing.T) {
	tests := []struct {
		name   string
		values []int
		size   int
		want   [][]int
	}{
		{"ровно", []int{1, 2, 3, 4}, 2, [][]int{{1, 2}, {3, 4}}},
		{"с хвостом", []int{1, 2, 3, 4, 5}, 2, [][]int{{1, 2}, {3, 4}, {5}}},
		{"пачка больше потока", []int{7, 8}, 5, [][]int{{7, 8}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := collect(t, Batch(feed(tt.values...), tt.size))
			if !slices.EqualFunc(got, tt.want, slices.Equal) {
				t.Errorf("Batch(%v, %d) = %v, want %v", tt.values, tt.size, got, tt.want)
			}
		})
	}
}
