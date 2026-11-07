package task

import (
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

// recorder запоминает, сколько фейкового времени прошло к каждому вызову f.
type recorder struct {
	mu    sync.Mutex
	start time.Time
	at    []time.Duration
}

func newRecorder() *recorder { return &recorder{start: time.Now()} }

func (r *recorder) fire() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.at = append(r.at, time.Since(r.start))
}

func (r *recorder) calls() []time.Duration {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]time.Duration(nil), r.at...)
}

// sleep двигает фейковые часы и ждёт, пока горутины таймеров отработают.
func sleep(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func wantCalls(t *testing.T, r *recorder, want ...time.Duration) {
	t.Helper()
	got := r.calls()
	if len(got) != len(want) {
		t.Fatalf("f вызвана в моменты %v, want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("f вызвана в моменты %v, want %v", got, want)
		}
	}
}

func TestSingleTrigger(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(100*time.Millisecond, 0, r.fire)
		defer d.Stop()

		d.Trigger()
		sleep(100*time.Millisecond - time.Nanosecond)
		wantCalls(t, r) // ещё рано
		sleep(time.Nanosecond)
		wantCalls(t, r, 100*time.Millisecond)
	})
}

func TestTriggersResetTimer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(100*time.Millisecond, 0, r.fire)
		defer d.Stop()

		d.Trigger()                  // 0
		sleep(60 * time.Millisecond) // 60
		d.Trigger()
		sleep(60 * time.Millisecond) // 120
		d.Trigger()
		sleep(time.Second)
		wantCalls(t, r, 220*time.Millisecond)
	})
}

func TestMaxWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(100*time.Millisecond, 250*time.Millisecond, r.fire)
		defer d.Stop()

		// События каждые 50 мс: без maxWait f не вызвалась бы никогда.
		for range 8 {
			d.Trigger()
			sleep(50 * time.Millisecond)
		}
		wantCalls(t, r, 250*time.Millisecond)
	})
}

func TestStop(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(time.Second, 0, r.fire)
		d.Trigger()
		sleep(500 * time.Millisecond)
		if !d.Stop() {
			t.Error("Stop() = false при запланированном вызове, want true")
		}
		sleep(time.Hour)
		wantCalls(t, r)
	})
}
