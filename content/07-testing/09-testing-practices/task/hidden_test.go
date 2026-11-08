package task

import (
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"
	"time"
)

// countingDeps считает вызовы зависимостей.
type countingDeps struct {
	mu        sync.Mutex
	now, ids  int
	clock     *FakeClock
	generator func() string
}

func newCounting() *countingDeps {
	return &countingDeps{clock: NewFakeClock(t0), generator: SeqIDs("C")}
}

func (c *countingDeps) Now() time.Time {
	c.mu.Lock()
	c.now++
	c.mu.Unlock()
	return c.clock.Now()
}

func (c *countingDeps) ID() string {
	c.mu.Lock()
	c.ids++
	c.mu.Unlock()
	return c.generator()
}

func TestHiddenInvalidDoesNotCallDeps(t *testing.T) {
	deps := newCounting()
	d := NewDesk(deps.Now, deps.ID)
	for _, in := range []struct {
		title string
		p     int
	}{{"", 2}, {"   ", 2}, {"x", 0}, {"x", -1}, {"x", 4}} {
		if _, err := d.Open(in.title, in.p); !errors.Is(err, ErrInvalid) {
			t.Errorf("Open(%q, %d) error = %v, want ErrInvalid", in.title, in.p, err)
		}
	}
	if deps.now != 0 || deps.ids != 0 {
		t.Errorf("на неверных входах вызвано now %d раз, newID %d раз; want 0 и 0", deps.now, deps.ids)
	}
	if q := d.Queue(); len(q) != 0 {
		t.Errorf("неверные тикеты сохранены: %+v", q)
	}
}

func TestHiddenOpenCallsDepsOnce(t *testing.T) {
	deps := newCounting()
	d := NewDesk(deps.Now, deps.ID)
	got, err := d.Open("x", 1)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if deps.now != 1 || deps.ids != 1 {
		t.Errorf("Open вызвал now %d раз, newID %d раз; want 1 и 1", deps.now, deps.ids)
	}
	if want := (Ticket{ID: "C-1", Title: "x", Priority: 1, Created: t0}); got != want {
		t.Errorf("Open = %+v, want %+v", got, want)
	}
}

func TestHiddenIDTieBreakIsStringOrder(t *testing.T) {
	d, _ := newTestDesk(t)
	for i := range 12 {
		if _, err := d.Open(fmt.Sprintf("t%d", i), 2); err != nil {
			t.Fatalf("Open: %v", err)
		}
	}
	want := []string{"T-1", "T-10", "T-11", "T-12", "T-2", "T-3", "T-4", "T-5", "T-6", "T-7", "T-8", "T-9"}
	for range 20 { // порядок обхода map меняется от вызова к вызову
		if got := ids(d.Queue()); !slices.Equal(got, want) {
			t.Fatalf("Queue() = %v, want %v", got, want)
		}
	}
}

func TestHiddenQueueFullOrder(t *testing.T) {
	d, clock := newTestDesk(t)
	d.Open("a", 2) // T-1 @0
	d.Open("b", 1) // T-2 @0
	clock.Advance(-time.Minute)
	d.Open("c", 2) // T-3 @-1m — раньше T-1
	clock.Advance(2 * time.Minute)
	d.Open("d", 3) // T-4 @+1m
	d.Open("e", 2) // T-5 @+1m
	want := []string{"T-4", "T-3", "T-1", "T-5", "T-2"}
	if got := ids(d.Queue()); !slices.Equal(got, want) {
		t.Errorf("Queue() = %v, want %v", got, want)
	}
}

func TestHiddenQueueIsCopy(t *testing.T) {
	d, _ := newTestDesk(t)
	if q := d.Queue(); len(q) != 0 {
		t.Errorf("Queue() пустого Desk = %v, want []", q)
	}
	d.Open("a", 1)
	q := d.Queue()
	if len(q) != 1 {
		t.Fatalf("len(Queue()) = %d, want 1", len(q))
	}
	q[0].Title = "испорчено"
	if got := d.Queue()[0].Title; got != "a" {
		t.Errorf("изменение среза из Queue повлияло на Desk: Title = %q", got)
	}
}

func TestHiddenExpire(t *testing.T) {
	d, clock := newTestDesk(t)
	d.Open("a", 1) // T-1 @0
	clock.Advance(10 * time.Minute)
	d.Open("b", 2) // T-2 @10m
	clock.Advance(10 * time.Minute)
	d.Open("c", 3) // T-3 @20m
	if n := d.Expire(time.Hour); n != 0 {
		t.Errorf("Expire(1h) = %d, want 0", n)
	}
	if n := d.Expire(10*time.Minute + time.Nanosecond); n != 1 {
		t.Errorf("Expire(10m1ns) = %d, want 1", n)
	}
	if n := d.Expire(10 * time.Minute); n != 1 {
		t.Errorf("Expire(10m) = %d, want 1", n)
	}
	if got, want := ids(d.Queue()), []string{"T-3"}; !slices.Equal(got, want) {
		t.Errorf("Queue() = %v, want %v", got, want)
	}
	if n := d.Expire(0); n != 1 {
		t.Errorf("Expire(0) = %d, want 1", n)
	}
	if n := d.Expire(0); n != 0 {
		t.Errorf("повторный Expire(0) = %d, want 0", n)
	}
}

func TestHiddenFakeClock(t *testing.T) {
	c := NewFakeClock(t0)
	if got := c.Now(); !got.Equal(t0) {
		t.Errorf("Now() = %v, want %v", got, t0)
	}
	c.Advance(90 * time.Second)
	c.Advance(-30 * time.Second)
	if got, want := c.Now(), t0.Add(time.Minute); !got.Equal(want) {
		t.Errorf("Now() после Advance = %v, want %v", got, want)
	}
	other := NewFakeClock(t0)
	if got := other.Now(); !got.Equal(t0) {
		t.Errorf("второй FakeClock показывает %v, want %v: часы не должны делить состояние", got, t0)
	}
}

func TestHiddenNilDefaults(t *testing.T) {
	d := NewDesk(nil, nil)
	before := time.Now()
	a, err1 := d.Open("a", 1)
	b, err2 := d.Open("b", 1)
	if err1 != nil || err2 != nil {
		t.Fatalf("Open: %v, %v", err1, err2)
	}
	if a.ID == "" || a.ID == b.ID {
		t.Errorf("ID по умолчанию: %q и %q — want непустые и разные", a.ID, b.ID)
	}
	if a.Created.Before(before) || a.Created.After(time.Now()) {
		t.Errorf("Created = %v — want текущее время", a.Created)
	}
}

// Независимые экземпляры: параллельные тесты не видят тикеты друг друга.
func TestHiddenIndependentDesks(t *testing.T) {
	for i := range 4 {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			t.Parallel()
			d, _ := newTestDesk(t)
			for range 50 {
				d.Open("x", 1)
			}
			q := d.Queue()
			if len(q) != 50 || q[0].ID != "T-1" {
				t.Errorf("len = %d, первый %q; want 50 и T-1 — экземпляры делят состояние", len(q), firstID(q))
			}
		})
	}
}

func firstID(q []Ticket) string {
	if len(q) == 0 {
		return ""
	}
	return q[0].ID
}

func TestHiddenConcurrentDesk(t *testing.T) {
	clock := NewFakeClock(t0)
	d := NewDesk(clock.Now, SeqIDs("T"))
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 50 {
				d.Open("x", 2)
				clock.Advance(time.Millisecond)
				d.Queue()
			}
		})
	}
	wg.Wait()
	if n := len(d.Queue()); n != 400 {
		t.Fatalf("len(Queue()) = %d, want 400", n)
	}
	wg.Go(func() { d.Expire(time.Hour) })
	wg.Go(func() { d.Open("y", 1) })
	wg.Wait()
	if n := d.Expire(0); n != 401 {
		t.Errorf("Expire(0) = %d, want 401", n)
	}
}

func TestHiddenConcurrentSeqIDs(t *testing.T) {
	gen := SeqIDs("S")
	var mu sync.Mutex
	seen := map[string]bool{}
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				id := gen()
				mu.Lock()
				seen[id] = true
				mu.Unlock()
			}
		})
	}
	wg.Wait()
	for i := 1; i <= 800; i++ {
		if id := fmt.Sprintf("S-%d", i); !seen[id] {
			t.Fatalf("нет %s среди 800 ID (уникальных %d) — повторы или пропуски", id, len(seen))
		}
	}
}
