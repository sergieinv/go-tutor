package task

import (
	"testing"
	"time"
)

// within падает, если f не вернулась за щедрый срок: так видно,
// что тик заблокировался на сохранении, а не просто «медленный».
func within(t *testing.T, what string, f func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		f()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("%s не вернулся: тик ждёт сохранения", what)
	}
}

// slowSave — функция save, которая отдаёт снапшот в started
// и «пишет» до закрытия release.
type slowSave struct {
	started chan *Snapshot
	release chan struct{}
	err     error
}

func newSlowSave() *slowSave {
	return &slowSave{started: make(chan *Snapshot, 10), release: make(chan struct{})}
}

func (s *slowSave) save(snap *Snapshot) error {
	s.started <- snap
	<-s.release
	return s.err
}

func (s *slowSave) next(t *testing.T) *Snapshot {
	t.Helper()
	select {
	case snap := <-s.started:
		return snap
	case <-time.After(2 * time.Second):
		t.Fatal("save не вызвана")
		return nil
	}
}

func TestBlocks(t *testing.T) {
	w := NewWorld(func(*Snapshot) error { return nil })
	cases := []struct {
		x, z int
		b    uint16
	}{{0, 0, 1}, {15, 15, 2}, {16, 0, 3}, {-1, -1, 4}, {-16, 5, 5}, {100, -100, 6}}
	for _, c := range cases {
		w.SetBlock(c.x, c.z, c.b)
	}
	for _, c := range cases {
		if got := w.Block(c.x, c.z); got != c.b {
			t.Errorf("Block(%d, %d) = %d, want %d", c.x, c.z, got, c.b)
		}
	}
	if got := w.Block(1, 1); got != 0 {
		t.Errorf("пустая клетка: Block(1, 1) = %d, want 0", got)
	}
}

func TestSnapshotIsConsistent(t *testing.T) {
	s := newSlowSave()
	w := NewWorld(s.save)
	w.SetBlock(1, 2, 7)
	w.Tick()
	w.Tick()
	var ok bool
	within(t, "StartSave", func() { ok = w.StartSave() })
	if !ok {
		t.Fatal("StartSave = false, want true")
	}
	snap := s.next(t)
	// Тик продолжается, пока save «пишет».
	within(t, "тик во время сохранения", func() {
		for range 10 {
			w.Tick()
			w.SetBlock(1, 2, 8)
			w.SetBlock(40, 40, 9)
		}
	})
	if snap.Tick != 2 {
		t.Errorf("snap.Tick = %d, want 2", snap.Tick)
	}
	if got := snap.Block(1, 2); got != 7 {
		t.Errorf("snap.Block(1, 2) = %d, want 7 — изменение после снапшота попало в него", got)
	}
	if got := snap.Block(40, 40); got != 0 {
		t.Errorf("snap.Block(40, 40) = %d, want 0", got)
	}
	if got := w.Block(1, 2); got != 8 {
		t.Errorf("w.Block(1, 2) = %d, want 8", got)
	}
	close(s.release)
	if err := w.Wait(); err != nil {
		t.Errorf("Wait = %v, want nil", err)
	}
}

func TestOneSaveAtATime(t *testing.T) {
	s := newSlowSave()
	w := NewWorld(s.save)
	if !w.StartSave() {
		t.Fatal("первый StartSave = false")
	}
	s.next(t)
	if w.StartSave() {
		t.Fatal("второй StartSave во время записи = true, want false")
	}
	close(s.release)
	w.Wait()
	w.Tick()
	if !w.StartSave() {
		t.Fatal("StartSave после Wait = false, want true")
	}
	if snap := s.next(t); snap.Tick != 1 {
		t.Errorf("snap.Tick = %d, want 1", snap.Tick)
	}
	w.Wait()
}
