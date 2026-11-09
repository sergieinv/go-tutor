package task

import (
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestNowCalledTwicePerRequest(t *testing.T) {
	clk, snaps := newClock(), &snapshots{}
	c := &Capture{Threshold: time.Second, Now: clk.Now, Snapshot: snaps.take}
	h := c.Middleware(handlerTaking(clk, 2*time.Second))
	serve(h, "GET", "/a")
	serve(h, "GET", "/b")
	if clk.calls != 4 {
		t.Fatalf("Now вызван %d раз за 2 запроса, want 4", clk.calls)
	}
}

func TestReasonFormat(t *testing.T) {
	clk, snaps := newClock(), &snapshots{}
	c := &Capture{Threshold: time.Second, Now: clk.Now, Snapshot: snaps.take}
	serve(c.Middleware(handlerTaking(clk, 1500*time.Millisecond)), "POST", "/orders?debug=1")
	if got := snaps.list(); len(got) != 1 || got[0] != "POST /orders 1.5s" {
		t.Fatalf("снимки = %q, want [\"POST /orders 1.5s\"] (путь без query)", got)
	}
}

func TestSnapshotErrorStillCountsAsAttempt(t *testing.T) {
	clk := newClock()
	var calls int
	c := &Capture{
		Threshold: time.Second,
		Cooldown:  10 * time.Second,
		Now:       clk.Now,
		Snapshot: func(string) error {
			calls++
			if calls == 1 {
				return errors.New("диск переполнен")
			}
			return nil
		},
	}
	slow := c.Middleware(handlerTaking(clk, 2*time.Second))
	serve(slow, "GET", "/a") // ошибка, но попытка в t0+2s
	serve(slow, "GET", "/b") // t0+4s — в пределах cooldown
	if calls != 1 || c.Captured() != 0 {
		t.Fatalf("после неудачного снимка: вызовов Snapshot %d, Captured %d, want 1 и 0", calls, c.Captured())
	}
	clk.Advance(10 * time.Second)
	serve(slow, "GET", "/c") // t0+16s
	if calls != 2 || c.Captured() != 1 {
		t.Fatalf("после cooldown: вызовов Snapshot %d, Captured %d, want 2 и 1", calls, c.Captured())
	}
}

func TestNilSnapshot(t *testing.T) {
	clk := newClock()
	c := &Capture{Threshold: time.Second, Now: clk.Now}
	rec := serve(c.Middleware(handlerTaking(clk, time.Hour)), "GET", "/x")
	if rec.Code != http.StatusCreated || c.Captured() != 0 {
		t.Fatalf("Snapshot == nil: код %d, Captured %d", rec.Code, c.Captured())
	}
}

func TestNilNowUsesRealClock(t *testing.T) {
	snaps := &snapshots{}
	c := &Capture{Threshold: time.Millisecond, Snapshot: snaps.take}
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Millisecond) // не меньше 5 мс по настоящим часам
	}))
	serve(h, "GET", "/sleep")
	if got := snaps.list(); len(got) != 1 {
		t.Fatalf("Now == nil: снимки %q, want один — нужен time.Now", got)
	}
}

// stepClock — каждое показание на секунду позже предыдущего: любой
// запрос длится не меньше секунды, сколько бы горутин ни шло.
type stepClock struct{ n atomic.Int64 }

func (s *stepClock) Now() time.Time { return t0.Add(time.Duration(s.n.Add(1)) * time.Second) }

// Пока идёт снимок, другие медленные запросы его пропускают и не ждут.
func TestOneSnapshotAtATime(t *testing.T) {
	var clk stepClock
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int64
	c := &Capture{
		Threshold: time.Second,
		Now:       clk.Now,
		Snapshot: func(string) error {
			if calls.Add(1) == 1 {
				close(entered)
				<-release
			}
			return nil
		},
	}
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))

	first := make(chan struct{})
	go func() {
		defer close(first)
		serve(h, "GET", "/first")
	}()
	select {
	case <-entered:
	case <-first:
		t.Fatal("медленный запрос не вызвал Snapshot")
	}

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() { serve(h, "GET", "/other") })
	}
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		close(release)
		t.Fatal("запросы ждут, пока закончится чужой снимок, — пропускайте снимок сразу")
	}
	if n := calls.Load(); n != 1 {
		close(release)
		t.Fatalf("пока шёл снимок, Snapshot вызван ещё %d раз", n-1)
	}
	close(release)
	<-first

	serve(h, "GET", "/after") // снимок закончен, cooldown нет — можно снова
	if n, got := calls.Load(), c.Captured(); n != 2 || got != 2 {
		t.Fatalf("после снимка: вызовов %d, Captured %d, want 2 и 2", n, got)
	}
}

func TestConcurrentCooldown(t *testing.T) {
	var clk stepClock
	var calls atomic.Int64
	c := &Capture{
		Threshold: time.Second,
		Cooldown:  time.Hour,
		Now:       clk.Now,
		Snapshot:  func(string) error { calls.Add(1); return nil },
	}
	h := c.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	var wg sync.WaitGroup
	for range 64 {
		wg.Go(func() {
			for range 5 {
				serve(h, "GET", "/load")
				_ = c.Captured()
			}
		})
	}
	wg.Wait()
	if n, got := calls.Load(), c.Captured(); n != 1 || got != 1 {
		t.Fatalf("320 медленных запросов за «минуты» при Cooldown = 1h: вызовов %d, Captured %d, want 1 и 1", n, got)
	}
}
