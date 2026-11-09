package task

import (
	"fmt"
	"slices"
	"sync"
	"testing"
)

func TestNegativeCapacityPanics(t *testing.T) {
	if r := panics(func() { New[int](-1) }); r == nil {
		t.Fatal("New(-1) не запаниковал")
	}
}

func TestCloseTwicePanics(t *testing.T) {
	c := New[int](0)
	c.Close()
	if r := panics(c.Close); r != "close of closed channel" {
		t.Fatalf("повторный Close: паника %v, want \"close of closed channel\"", r)
	}
}

func TestCloseWakesAllReceivers(t *testing.T) {
	c := New[int](4)
	var wg sync.WaitGroup
	results := make([]string, 3)
	for i := range 3 {
		wg.Go(func() {
			v, ok := c.Recv()
			results[i] = fmt.Sprint(v, ok)
		})
	}
	waitUntil(t, "три получателя ждут", func() bool { _, r := c.Waiting(); return r == 3 })
	c.Close()
	wg.Wait()
	for i, r := range results {
		if r != "0 false" {
			t.Errorf("получатель %d после Close получил %q, want \"0 false\"", i, r)
		}
	}
	if s, r := c.Waiting(); s != 0 || r != 0 {
		t.Errorf("Waiting после Close = %d, %d, want 0, 0", s, r)
	}
}

func TestCloseWakesSendersWithPanic(t *testing.T) {
	c := New[int](1)
	c.Send(1) // буфер полон
	got := make(chan any, 2)
	for range 2 {
		go func() { got <- panics(func() { c.Send(2) }) }()
	}
	waitUntil(t, "два отправителя ждут", func() bool { s, _ := c.Waiting(); return s == 2 })
	c.Close()
	for range 2 {
		if r := <-got; r != "send on closed channel" {
			t.Fatalf("ждущий отправитель после Close: паника %v, want \"send on closed channel\"", r)
		}
	}
	if v, ok := c.Recv(); v != 1 || !ok {
		t.Fatalf("Recv = (%d, %v), want (1, true): значение из буфера не теряется", v, ok)
	}
}

func TestWaitingReceiversFIFO(t *testing.T) {
	c := New[int](2)
	var wg sync.WaitGroup
	got := make([]int, 3)
	for i := range 3 {
		wg.Go(func() { got[i], _ = c.Recv() })
		waitUntil(t, fmt.Sprintf("получатель %d ждёт", i), func() bool { _, r := c.Waiting(); return r == i+1 })
	}
	for v := range 3 {
		c.Send(v * 10)
	}
	wg.Wait()
	if want := []int{0, 10, 20}; !slices.Equal(got, want) {
		t.Fatalf("получатели в порядке прихода получили %v, want %v", got, want)
	}
}

func TestWaitingSendersFIFO(t *testing.T) {
	c := New[int](1)
	c.Send(100)
	var wg sync.WaitGroup
	for i := range 3 {
		wg.Go(func() { c.Send(i) })
		waitUntil(t, fmt.Sprintf("отправитель %d ждёт", i), func() bool { s, _ := c.Waiting(); return s == i+1 })
	}
	var got []int
	for range 4 {
		v, _ := c.Recv()
		got = append(got, v)
		if c.Len() > c.Cap() {
			t.Fatalf("Len %d больше Cap %d", c.Len(), c.Cap())
		}
	}
	wg.Wait()
	if want := []int{100, 0, 1, 2}; !slices.Equal(got, want) {
		t.Fatalf("порядок %v, want %v", got, want)
	}
}

func TestDirectHandoffBypassesBuffer(t *testing.T) {
	c := New[int](2)
	got := make(chan int, 1)
	go func() { v, _ := c.Recv(); got <- v }()
	waitUntil(t, "получатель ждёт", func() bool { _, r := c.Waiting(); return r == 1 })
	c.Send(5)
	if n := c.Len(); n != 0 {
		t.Fatalf("Len после отправки ждущему получателю = %d, want 0: значение передаётся напрямую", n)
	}
	if v := <-got; v != 5 {
		t.Fatalf("получатель получил %d, want 5", v)
	}
}

func TestRecvPullsWaitingSenderIntoBuffer(t *testing.T) {
	c := New[string](1)
	c.Send("first")
	var wg sync.WaitGroup
	wg.Go(func() { c.Send("second") })
	waitUntil(t, "отправитель ждёт", func() bool { s, _ := c.Waiting(); return s == 1 })
	if v, ok := c.Recv(); v != "first" || !ok {
		t.Fatalf("Recv = (%q, %v), want (\"first\", true)", v, ok)
	}
	// Ждущий отправитель переложен в буфер в момент Recv.
	if s, _ := c.Waiting(); s != 0 || c.Len() != 1 {
		t.Fatalf("после Recv: ждущих отправителей %d, Len %d; want 0 и 1", s, c.Len())
	}
	wg.Wait()
	if v, _ := c.Recv(); v != "second" {
		t.Fatalf("второй Recv = %q, want \"second\"", v)
	}
}

func TestUnbufferedRecvTakesFromSender(t *testing.T) {
	c := New[int](0)
	var wg sync.WaitGroup
	for i := range 2 {
		wg.Go(func() { c.Send(i + 1) })
		waitUntil(t, "отправитель ждёт", func() bool { s, _ := c.Waiting(); return s == i+1 })
	}
	a, _ := c.Recv()
	b, _ := c.Recv()
	wg.Wait()
	if a != 1 || b != 2 || c.Len() != 0 {
		t.Fatalf("Recv = %d, %d, Len %d; want 1, 2, 0", a, b, c.Len())
	}
}

func TestConcurrentProducersConsumers(t *testing.T) {
	for _, capacity := range []int{0, 1, 8} {
		t.Run(fmt.Sprint("cap=", capacity), func(t *testing.T) {
			c := New[int](capacity)
			const producers, perProducer, consumers = 4, 500, 4
			var prod, cons sync.WaitGroup
			for p := range producers {
				prod.Go(func() {
					for i := range perProducer {
						c.Send(p*perProducer + i)
					}
				})
			}
			seen := make([][]int, consumers)
			for k := range consumers {
				cons.Go(func() {
					for {
						v, ok := c.Recv()
						if !ok {
							return
						}
						seen[k] = append(seen[k], v)
					}
				})
			}
			prod.Wait()
			c.Close()
			cons.Wait()
			var all []int
			for _, s := range seen {
				all = append(all, s...)
			}
			slices.Sort(all)
			if len(all) != producers*perProducer {
				t.Fatalf("получено %d значений, want %d", len(all), producers*perProducer)
			}
			for i, v := range all {
				if v != i {
					t.Fatalf("значение %d потеряно или пришло дважды", i)
				}
			}
		})
	}
}
