package task

import (
	"fmt"
	"sync"
	"testing"
)

func TestReloadBeforeConfigSkipsLoad(t *testing.T) {
	s, calls := newCounted("never")
	c := &Config{Name: "reloaded"}
	s.Reload(c)
	if got := s.Config(); got != c {
		t.Fatalf("Config() = %+v, want конфигурацию из Reload", got)
	}
	if n := calls.Load(); n != 0 {
		t.Fatalf("load вызван %d раз, хотя Reload был раньше первого Config", n)
	}
}

// Читатели держат выданную конфигурацию и читают её поля, пока
// писатель публикует новые. Изменять уже выданный объект нельзя —
// детектор гонок это заметит.
func TestReloadWhileReading(t *testing.T) {
	s, _ := newCounted("v0")
	s.Config()
	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				c := s.Config()
				if c == nil {
					t.Error("Config() = nil")
					return
				}
				// Имя и лимит записаны до публикации — они согласованы.
				want := 100
				if c.Name != "v0" {
					fmt.Sscanf(c.Name, "v%d", &want)
				}
				if c.Limits["rps"] != want {
					t.Errorf("конфигурация %q: rps = %d, want %d", c.Name, c.Limits["rps"], want)
					return
				}
			}
		})
	}
	for i := 1; i <= 200; i++ {
		s.Reload(&Config{Name: fmt.Sprintf("v%d", i), Limits: map[string]int{"rps": i}})
	}
	last := &Config{Name: "v201", Limits: map[string]int{"rps": 201}}
	s.Reload(last)
	close(stop)
	wg.Wait()
	if c := s.Config(); c != last {
		t.Fatalf("после последнего Reload Config() = %+v, want %+v", c, last)
	}
}

func TestReloadRacesWithFirstConfig(t *testing.T) {
	for range 50 {
		s, calls := newCounted("loaded")
		reloaded := &Config{Name: "reloaded"}
		var wg sync.WaitGroup
		for range 4 {
			wg.Go(func() {
				if c := s.Config(); c == nil {
					t.Error("Config() = nil")
				}
			})
		}
		wg.Go(func() { s.Reload(reloaded) })
		wg.Wait()
		// Reload завершился — дальше видна только его конфигурация,
		// результат load её не затирает.
		if c := s.Config(); c != reloaded {
			t.Fatalf("после Reload Config() = %+v, want %+v", c, reloaded)
		}
		if n := calls.Load(); n > 1 {
			t.Fatalf("load вызван %d раз, want не больше 1", n)
		}
	}
}

func TestMarkReadyConcurrent(t *testing.T) {
	s, _ := newCounted("x")
	if s.Ready() {
		t.Fatal("Ready() = true до MarkReady")
	}
	var waiters sync.WaitGroup
	for range 4 {
		waiters.Go(func() {
			<-s.Done()
			if !s.Ready() {
				t.Error("дождались Done, но Ready() = false")
			}
		})
	}
	var wg sync.WaitGroup
	for range 16 {
		wg.Go(func() {
			s.MarkReady()
			if !s.Ready() {
				t.Error("после MarkReady Ready() = false")
			}
		})
	}
	wg.Wait()
	waiters.Wait()
}

func TestDoneStable(t *testing.T) {
	s, _ := newCounted("x")
	a, b := s.Done(), s.Done()
	if a == nil || a != b {
		t.Fatal("Done() должен возвращать один и тот же не-nil канал")
	}
	select {
	case <-a:
		t.Fatal("Done() закрыт до MarkReady")
	default:
	}
}

func TestReadyPollingWhileMarking(t *testing.T) {
	s, _ := newCounted("x")
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for !s.Ready() {
				s.Hit()
			}
		})
	}
	s.MarkReady()
	wg.Wait()
	if !s.Ready() {
		t.Fatal("Ready() = false после MarkReady")
	}
}
