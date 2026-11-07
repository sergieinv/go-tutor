package task

import (
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

func TestHiddenNewSeriesAfterFire(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(100*time.Millisecond, 150*time.Millisecond, r.fire)
		defer d.Stop()

		d.Trigger() // серия 1: 0 → 100
		sleep(300 * time.Millisecond)
		// Серия 2 начинается в 300: её maxWait считается от 300, а не от 0.
		d.Trigger()
		sleep(80 * time.Millisecond) // 380
		d.Trigger()
		sleep(80 * time.Millisecond) // 460 → лимит 300+150 = 450 уже прошёл
		wantCalls(t, r, 100*time.Millisecond, 450*time.Millisecond)
	})
}

func TestHiddenMaxWaitLessThanWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(time.Second, 300*time.Millisecond, r.fire)
		defer d.Stop()

		d.Trigger()
		sleep(299 * time.Millisecond)
		wantCalls(t, r)
		sleep(time.Millisecond)
		wantCalls(t, r, 300*time.Millisecond)
	})
}

func TestHiddenMaxWaitExactBoundary(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(100*time.Millisecond, 200*time.Millisecond, r.fire)
		defer d.Stop()

		d.Trigger()                  // в 0: вызов в 100
		sleep(90 * time.Millisecond) // 90
		d.Trigger()                  // вызов в 190
		sleep(90 * time.Millisecond) // 180
		d.Trigger()                  // min(280, 0+200) = 200
		sleep(19 * time.Millisecond)
		wantCalls(t, r)
		sleep(time.Millisecond)
		wantCalls(t, r, 200*time.Millisecond)
		// Серия закончилась; ничего больше не ожидается.
		sleep(time.Second)
		wantCalls(t, r, 200*time.Millisecond)
	})
}

func TestHiddenStopReturnValues(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(100*time.Millisecond, 0, r.fire)
		if d.Stop() {
			t.Error("Stop() без Trigger = true, want false")
		}
		if d.Stop() {
			t.Error("повторный Stop() = true, want false")
		}

		d2 := NewDebouncer(100*time.Millisecond, 0, r.fire)
		d2.Trigger()
		sleep(200 * time.Millisecond) // вызов уже состоялся
		if d2.Stop() {
			t.Error("Stop() после состоявшегося вызова = true, want false")
		}
		wantCalls(t, r, 100*time.Millisecond)
	})
}

func TestHiddenTriggerAfterStopIgnored(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(100*time.Millisecond, 0, r.fire)
		d.Trigger()
		d.Stop()
		d.Trigger()
		d.Trigger()
		sleep(time.Hour)
		wantCalls(t, r)
		if d.Stop() {
			t.Error("Stop() после выключения = true, want false")
		}
	})
}

func TestHiddenStopAtFireMoment(t *testing.T) {
	// Stop в тот же момент, когда срабатывает таймер: f вызвана
	// не больше одного раза, и после Stop новых вызовов нет.
	synctest.Test(t, func(t *testing.T) {
		var n atomic.Int32
		d := NewDebouncer(100*time.Millisecond, 0, func() { n.Add(1) })
		d.Trigger()
		time.Sleep(100 * time.Millisecond) // без Wait: таймер и тест проснулись вместе
		d.Stop()
		sleep(time.Hour)
		if got := n.Load(); got > 1 {
			t.Errorf("f вызвана %d раз, want не больше 1", got)
		}
	})
}

func TestHiddenRetriggerFromCallback(t *testing.T) {
	// f сама вызывает Trigger — это новая серия, без взаимоблокировки.
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		var d *Debouncer
		var mu sync.Mutex
		count := 0
		d = NewDebouncer(100*time.Millisecond, 0, func() {
			r.fire()
			mu.Lock()
			count++
			again := count < 3
			mu.Unlock()
			if again {
				d.Trigger()
			}
		})
		defer d.Stop()
		d.Trigger()
		sleep(time.Second)
		wantCalls(t, r, 100*time.Millisecond, 200*time.Millisecond, 300*time.Millisecond)
	})
}

func TestHiddenConcurrentTriggers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		r := newRecorder()
		d := NewDebouncer(50*time.Millisecond, 0, r.fire)
		defer d.Stop()

		var wg sync.WaitGroup
		for range 20 {
			wg.Go(func() {
				for range 5 {
					d.Trigger()
				}
			})
		}
		wg.Wait()
		sleep(50*time.Millisecond - time.Nanosecond)
		wantCalls(t, r)
		sleep(time.Second)
		wantCalls(t, r, 50*time.Millisecond)
	})
}

func TestHiddenRunsInOtherGoroutine(t *testing.T) {
	// f вызывается не из Trigger: Trigger возвращается сразу.
	synctest.Test(t, func(t *testing.T) {
		var called atomic.Bool
		d := NewDebouncer(time.Millisecond, 0, func() { called.Store(true) })
		defer d.Stop()
		d.Trigger()
		if called.Load() {
			t.Fatal("f вызвана прямо из Trigger, а должна — через wait")
		}
		sleep(time.Millisecond)
		if !called.Load() {
			t.Fatal("f не вызвана через wait")
		}
	})
}
