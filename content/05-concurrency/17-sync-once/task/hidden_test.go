package task

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestCacheConcurrentSameKey(t *testing.T) {
	var calls atomic.Int32
	release := make(chan struct{})
	c := NewCache(func(k int) (int, error) {
		calls.Add(1)
		<-release // загрузка «висит», пока все горутины не придут
		return k * 10, nil
	})

	var wg sync.WaitGroup
	results := make([]int, 50)
	for i := range results {
		wg.Go(func() {
			v, _ := c.Get(4)
			results[i] = v
		})
	}
	time.Sleep(20 * time.Millisecond)
	close(release)
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("load вызван %d раз для одного ключа, want 1", n)
	}
	for i, v := range results {
		if v != 40 {
			t.Fatalf("горутина %d получила %d, want 40", i, v)
		}
	}
}

func TestCacheKeysLoadInParallel(t *testing.T) {
	release := make(chan struct{})
	defer close(release)
	c := NewCache(func(k string) (string, error) {
		if k == "slow" {
			<-release
		}
		return k, nil
	})

	go c.Get("slow")
	time.Sleep(20 * time.Millisecond) // slow уже грузится

	done := make(chan string)
	go func() {
		v, _ := c.Get("fast")
		done <- v
	}()
	select {
	case v := <-done:
		if v != "fast" {
			t.Fatalf("Get(fast) = %q, want fast", v)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Get(fast) ждёт, пока загрузится другой ключ: не держите мьютекс во время load")
	}
}

func TestCachePanicRepeats(t *testing.T) {
	var calls atomic.Int32
	c := NewCache(func(k string) (int, error) {
		calls.Add(1)
		panic("load failed: " + k)
	})
	get := func() (p any) {
		defer func() { p = recover() }()
		c.Get("x")
		return nil
	}
	for i := 1; i <= 2; i++ {
		if p := get(); p != "load failed: x" {
			t.Fatalf("Get #%d: паника %v, want %q", i, p, "load failed: x")
		}
	}
	if n := calls.Load(); n != 1 {
		t.Errorf("load вызван %d раз, want 1: после паники load не повторяется", n)
	}
	// Паника одного ключа не ломает другие.
	c2 := NewCache(func(k string) (string, error) {
		if k == "bad" {
			panic("bad")
		}
		return k, nil
	})
	func() {
		defer func() { recover() }()
		c2.Get("bad")
	}()
	if v, err := c2.Get("good"); v != "good" || err != nil {
		t.Errorf("после паники другого ключа Get(good) = (%q, %v), want (good, nil)", v, err)
	}
}
