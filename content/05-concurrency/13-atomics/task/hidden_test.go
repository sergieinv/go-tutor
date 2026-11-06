package task

import (
	"sync"
	"testing"
)

func TestGaugeNegative(t *testing.T) {
	var g Gauge
	for _, v := range []int64{-10, -3, -7} {
		g.Observe(v)
	}
	if m, ok := g.Max(); !ok || m != -3 {
		t.Fatalf("Max() = (%d, %v), want (-3, true)", m, ok)
	}
}

func TestGaugeZeroIsAValue(t *testing.T) {
	var g Gauge
	g.Observe(0)
	if m, ok := g.Max(); !ok || m != 0 {
		t.Fatalf("Max() после Observe(0) = (%d, %v), want (0, true)", m, ok)
	}
	g.Observe(-1)
	if m, _ := g.Max(); m != 0 {
		t.Fatalf("Max() = %d, want 0", m)
	}
}

func TestGaugeConcurrent(t *testing.T) {
	const workers, per = 16, 1000
	var g Gauge
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range per {
				// Максимум — у одного наблюдения одной горутины.
				g.Observe(int64(i*workers + w - 5000))
				if _, ok := g.Max(); !ok {
					t.Error("Max() вернул false после Observe")
					return
				}
			}
		}()
	}
	wg.Wait()
	if n := g.Count(); n != workers*per {
		t.Errorf("Count() = %d, want %d", n, workers*per)
	}
	want := int64((per-1)*workers + workers - 1 - 5000)
	if m, ok := g.Max(); !ok || m != want {
		t.Errorf("Max() = (%d, %v), want (%d, true)", m, ok, want)
	}
}

func TestGaugeMaxNeverDecreases(t *testing.T) {
	var g Gauge
	var wg sync.WaitGroup
	stop := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		var prev int64
		seen := false
		for {
			select {
			case <-stop:
				return
			default:
			}
			m, ok := g.Max()
			if !ok {
				continue
			}
			if seen && m < prev {
				t.Errorf("Max() уменьшился: %d -> %d", prev, m)
				return
			}
			prev, seen = m, true
		}
	}()
	var writers sync.WaitGroup
	for w := range 8 {
		writers.Add(1)
		go func() {
			defer writers.Done()
			for i := range 2000 {
				g.Observe(int64((i*7 + w*13) % 1000))
			}
		}()
	}
	writers.Wait()
	close(stop)
	wg.Wait()
	if m, _ := g.Max(); m != 999 {
		t.Fatalf("Max() = %d, want 999", m)
	}
}
