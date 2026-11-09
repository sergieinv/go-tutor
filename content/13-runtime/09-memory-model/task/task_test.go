package task

import (
	"sync"
	"sync/atomic"
	"testing"
)

func newCounted(name string) (*Service, *atomic.Int32) {
	var calls atomic.Int32
	s := New(func() *Config {
		calls.Add(1)
		return &Config{Name: name, Limits: map[string]int{"rps": 100}}
	})
	return s, &calls
}

func TestSequential(t *testing.T) {
	s, calls := newCounted("initial")
	if n := calls.Load(); n != 0 {
		t.Fatalf("New вызвал load %d раз, а должен лениво — при первом Config", n)
	}
	if c := s.Config(); c == nil || c.Name != "initial" {
		t.Fatalf("Config() = %+v, want конфигурацию из load", c)
	}
	s.Config()
	if n := calls.Load(); n != 1 {
		t.Fatalf("load вызван %d раз, want 1", n)
	}

	next := &Config{Name: "next"}
	s.Reload(next)
	if c := s.Config(); c != next {
		t.Fatalf("после Reload Config() = %+v, want %+v", c, next)
	}

	if s.Ready() {
		t.Fatal("Ready() = true до MarkReady")
	}
	s.MarkReady()
	s.MarkReady() // повторный вызов безопасен
	if !s.Ready() {
		t.Fatal("Ready() = false после MarkReady")
	}
	select {
	case <-s.Done():
	default:
		t.Fatal("Done() не закрыт после MarkReady")
	}

	s.Hit()
	s.Hit()
	if n := s.Hits(); n != 2 {
		t.Fatalf("Hits() = %d, want 2", n)
	}
}

func TestConcurrentConfig(t *testing.T) {
	s, calls := newCounted("lazy")
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() {
			c := s.Config()
			if c == nil || c.Name != "lazy" || c.Limits["rps"] != 100 {
				t.Errorf("Config() = %+v", c)
			}
		})
	}
	wg.Wait()
	if n := calls.Load(); n != 1 {
		t.Fatalf("load вызван %d раз, want ровно 1", n)
	}
}

func TestConcurrentHits(t *testing.T) {
	s, _ := newCounted("x")
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			for range 500 {
				s.Hit()
				_ = s.Hits()
			}
		})
	}
	wg.Wait()
	if n := s.Hits(); n != 16*500 {
		t.Fatalf("Hits() = %d, want %d", n, 16*500)
	}
}
