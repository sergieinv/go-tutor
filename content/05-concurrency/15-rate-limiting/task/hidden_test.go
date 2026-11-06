package task

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func waitFor(d time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(d)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(time.Millisecond)
	}
	return cond()
}

func TestRefill(t *testing.T) {
	l := NewLimiter(10*time.Millisecond, 1)
	defer l.Stop()
	if !l.Allow() {
		t.Fatal("первый Allow = false, want true")
	}
	if !waitFor(2*time.Second, l.Allow) {
		t.Fatal("за 2 с при interval 10ms не появилось ни одного токена")
	}
}

func TestNoAccumulationOverBurst(t *testing.T) {
	const burst = 2
	l := NewLimiter(30*time.Millisecond, burst)
	defer l.Stop()
	for range burst {
		l.Allow()
	}
	time.Sleep(300 * time.Millisecond) // ~10 тиков, но ведро вмещает только 2
	n := 0
	for l.Allow() && n < 100 {
		n++
	}
	// Запас — burst, плюс, может быть, один тик за время цикла.
	if n < burst || n > burst+1 {
		t.Errorf("после простоя Allow пропустил %d запросов подряд, want %d: лишние токены нужно выбрасывать", n, burst)
	}
}

func TestBurstBelowOne(t *testing.T) {
	for _, burst := range []int{0, -3} {
		l := NewLimiter(time.Hour, burst)
		if !l.Allow() {
			t.Errorf("NewLimiter(burst=%d): первый Allow = false, want true (ёмкость 1)", burst)
		}
		if l.Allow() {
			t.Errorf("NewLimiter(burst=%d): второй Allow = true, want false (ёмкость 1)", burst)
		}
		l.Stop()
	}
}

func TestConcurrentWaitAndAllow(t *testing.T) {
	const (
		interval = 5 * time.Millisecond
		burst    = 4
		waiters  = 12
	)
	start := time.Now()
	l := NewLimiter(interval, burst)
	defer l.Stop()

	var wg sync.WaitGroup
	var allowed atomic.Int32
	for range waiters {
		wg.Go(func() { l.Wait() })
	}
	for range 50 {
		wg.Go(func() {
			if l.Allow() {
				allowed.Add(1)
			}
		})
	}
	wg.Wait()
	// Каждый токен достаётся одному вызову: всего выдано
	// waiters + allowed токенов, а за время d их не больше burst + d/interval.
	d := time.Since(start)
	total := waiters + int(allowed.Load())
	if limit := burst + int(d/interval); total > limit+1 {
		t.Errorf("за %v выдано %d токенов, а ведро могло выдать не больше %d", d, total, limit)
	}
}

func TestStopEndsRefill(t *testing.T) {
	before := runtime.NumGoroutine()
	l := NewLimiter(5*time.Millisecond, 1)
	l.Allow()
	l.Stop()
	if !waitFor(time.Second, func() bool { return runtime.NumGoroutine() <= before }) {
		t.Fatalf("горутин до NewLimiter: %d, после Stop: %d — горутина пополнения не завершилась", before, runtime.NumGoroutine())
	}
	// После Stop новых токенов нет (один мог успеть прийти до Stop).
	l.Allow()
	time.Sleep(50 * time.Millisecond)
	if l.Allow() {
		t.Error("после Stop в ведре появился новый токен")
	}
}

// После Stop токенов больше не будет: Wait не должен висеть вечно.
func TestWaitAfterStop(t *testing.T) {
	l := NewLimiter(time.Hour, 2)
	l.Stop()
	for i, want := range []bool{true, true, false, false} {
		res := make(chan bool, 1)
		go func() { res <- l.Wait() }()
		select {
		case got := <-res:
			if got != want {
				t.Fatalf("Wait №%d после Stop = %v, want %v (в ведре было 2 токена)", i+1, got, want)
			}
		case <-time.After(time.Second):
			t.Fatalf("Wait №%d после Stop заблокировался", i+1)
		}
	}
	if l.Allow() {
		t.Error("Allow после Stop на пустом ведре = true, want false")
	}
}

// Wait, который ждал токен в момент Stop, просыпается с false.
func TestStopWakesWaiter(t *testing.T) {
	l := NewLimiter(time.Hour, 1)
	l.Allow()
	res := make(chan bool, 1)
	go func() { res <- l.Wait() }()
	time.Sleep(20 * time.Millisecond)
	l.Stop()
	select {
	case got := <-res:
		if got {
			t.Error("Wait после Stop на пустом ведре = true, want false")
		}
	case <-time.After(time.Second):
		t.Fatal("Wait не проснулся после Stop")
	}
}
