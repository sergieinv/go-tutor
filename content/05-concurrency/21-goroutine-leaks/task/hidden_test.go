package task

import (
	"context"
	"errors"
	"maps"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

// waitGoroutines ждёт до 1 с, пока число горутин опустится до base,
// и возвращает, сколько горутин осталось сверх него.
func waitGoroutines(base int) int {
	deadline := time.Now().Add(time.Second)
	for {
		n := runtime.NumGoroutine()
		if n <= base || time.Now().After(deadline) {
			return n - base
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestLookupEmpty(t *testing.T) {
	got, err := Lookup(context.Background(), nil, func(string) string {
		t.Error("get не должна вызываться без ключей")
		return ""
	})
	if err != nil || got == nil || len(got) != 0 {
		t.Fatalf("Lookup(nil) = (%#v, %v), want (пустая не-nil map, nil)", got, err)
	}
}

func TestLookupConcurrent(t *testing.T) {
	keys := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	var started atomic.Int32
	all := make(chan struct{})
	got, err := Lookup(context.Background(), keys, func(k string) string {
		if started.Add(1) == int32(len(keys)) {
			close(all)
		}
		select {
		case <-all:
			return k + k
		case <-time.After(3 * time.Second):
			return "timeout"
		}
	})
	if err != nil {
		t.Fatalf("Lookup вернул ошибку %v", err)
	}
	want := map[string]string{}
	for _, k := range keys {
		want[k] = k + k
	}
	if !maps.Equal(got, want) {
		t.Fatalf("Lookup = %v, want %v", got, want)
	}
}

func TestLookupCancelNoLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	release := make(chan struct{})
	get := func(k string) string {
		if k == "fast" {
			return "ok"
		}
		<-release // медленный источник, отмену не слушает
		return "late"
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()
	type res struct {
		m   map[string]string
		err error
	}
	done := make(chan res, 1)
	go func() {
		m, err := Lookup(ctx, []string{"fast", "s1", "s2", "s3"}, get)
		done <- res{m, err}
	}()
	select {
	case r := <-done:
		if r.m != nil || !errors.Is(r.err, context.Canceled) {
			t.Fatalf("Lookup после отмены = (%v, %v), want (nil, context.Canceled)", r.m, r.err)
		}
	case <-time.After(2 * time.Second):
		close(release)
		t.Fatal("Lookup не вернулся после отмены ctx")
	}
	close(release) // медленные get возвращаются — их горутины должны завершиться
	if n := waitGoroutines(base); n > 0 {
		t.Fatalf("лишних горутин после возврата всех get: %d — опоздавший ответ некому принять", n)
	}
}

func TestTotalEmpty(t *testing.T) {
	base := runtime.NumGoroutine()
	if got := Total(nil, func(n int) int { return n }); got != 0 {
		t.Fatalf("Total(nil) = %d, want 0", got)
	}
	if n := waitGoroutines(base); n > 0 {
		t.Fatalf("лишних горутин после Total(nil): %d", n)
	}
}

func TestTotalNoLeak(t *testing.T) {
	base := runtime.NumGoroutine()
	nums := make([]int, 100)
	for i := range nums {
		nums[i] = i + 1
	}
	var calls atomic.Int32
	got := Total(nums, func(n int) int { calls.Add(1); return 2 * n })
	if got != 10100 {
		t.Fatalf("Total = %d, want 10100", got)
	}
	if c := calls.Load(); c != 100 {
		t.Fatalf("weight вызвана %d раз, want 100", c)
	}
	if n := waitGoroutines(base); n > 0 {
		t.Fatalf("лишних горутин после возврата Total: %d", n)
	}
}

func TestEveryStop(t *testing.T) {
	base := runtime.NumGoroutine()
	var calls atomic.Int32
	stop := Every(time.Millisecond, func() { calls.Add(1) })
	deadline := time.Now().Add(time.Second)
	for calls.Load() < 2 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	stop()
	after := calls.Load()
	time.Sleep(20 * time.Millisecond)
	if n := calls.Load(); n != after {
		t.Fatalf("после stop f вызвана ещё %d раз", n-after)
	}
	if n := waitGoroutines(base); n > 0 {
		t.Fatalf("лишних горутин после stop: %d — цикл тикера должен завершиться", n)
	}
}

func TestEveryNotBeforeInterval(t *testing.T) {
	var calls atomic.Int32
	stop := Every(time.Hour, func() { calls.Add(1) })
	time.Sleep(10 * time.Millisecond)
	stop()
	if n := calls.Load(); n != 0 {
		t.Fatalf("f вызвана %d раз раньше первого интервала", n)
	}
}

func TestEveryStopTwice(t *testing.T) {
	base := runtime.NumGoroutine()
	stop := Every(time.Millisecond, func() {})
	done := make(chan struct{})
	go func() {
		defer close(done)
		stop()
		stop()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("повторный вызов stop не вернулся")
	}
	if n := waitGoroutines(base); n > 0 {
		t.Fatalf("лишних горутин после stop: %d", n)
	}
}
