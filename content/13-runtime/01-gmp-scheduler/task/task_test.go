package task

import (
	"slices"
	"testing"
)

func newSched(nP int) *Sched {
	return &Sched{P: make([]P, nP), LocalCap: 256, GlobalEvery: 61}
}

// drain запускает FindRunnable на p, пока есть работа, и возвращает порядок.
func drain(t *testing.T, s *Sched, p int) []string {
	t.Helper()
	var order []string
	for range 1000 {
		g, ok := s.FindRunnable(p)
		if !ok {
			return order
		}
		order = append(order, g)
	}
	t.Fatalf("FindRunnable(%d) не закончил работу за 1000 шагов", p)
	return nil
}

func TestSpawnUsesRunNext(t *testing.T) {
	s := newSched(1)
	s.Spawn(0, "G1")
	if s.P[0].RunNext != "G1" || len(s.P[0].Local) != 0 {
		t.Fatalf("после Spawn(G1): RunNext=%q Local=%v, want RunNext=G1, Local=[]", s.P[0].RunNext, s.P[0].Local)
	}
	s.Spawn(0, "G2")
	s.Spawn(0, "G3")
	if s.P[0].RunNext != "G3" || !slices.Equal(s.P[0].Local, []string{"G1", "G2"}) {
		t.Fatalf("после Spawn(G1, G2, G3): RunNext=%q Local=%v, want RunNext=G3, Local=[G1 G2]", s.P[0].RunNext, s.P[0].Local)
	}
}

func TestRunNextFirstThenLocal(t *testing.T) {
	s := newSched(1)
	for _, g := range []string{"G1", "G2", "G3", "G4"} {
		s.Spawn(0, g)
	}
	got := drain(t, s, 0)
	want := []string{"G4", "G1", "G2", "G3"}
	if !slices.Equal(got, want) {
		t.Fatalf("порядок запуска %v, want %v: сначала runnext, потом локальная очередь с головы", got, want)
	}
	if s.P[0].Tick != 4 {
		t.Errorf("Tick = %d, want 4", s.P[0].Tick)
	}
}

func TestGlobalBatch(t *testing.T) {
	s := newSched(2)
	s.P[0].Tick = 1 // не момент «раз в 61»
	s.Global = []string{"A", "B", "C", "D", "E"}
	g, ok := s.FindRunnable(0)
	// Пачка: min(5, 5/2+1, 128) = 3 — A запускается, B и C — в локальную очередь.
	if !ok || g != "A" {
		t.Fatalf("FindRunnable = (%q, %v), want (A, true)", g, ok)
	}
	if !slices.Equal(s.P[0].Local, []string{"B", "C"}) || !slices.Equal(s.Global, []string{"D", "E"}) {
		t.Fatalf("после пачки Local=%v Global=%v, want Local=[B C], Global=[D E]", s.P[0].Local, s.Global)
	}
}

func TestStealHalf(t *testing.T) {
	s := newSched(2)
	s.P[0].Local = []string{"G1", "G2", "G3", "G4"}
	g, ok := s.FindRunnable(1)
	// Украдены G1 и G2 (половина с головы), G2 запускается.
	if !ok || g != "G2" {
		t.Fatalf("FindRunnable(1) = (%q, %v), want (G2, true)", g, ok)
	}
	if !slices.Equal(s.P[1].Local, []string{"G1"}) || !slices.Equal(s.P[0].Local, []string{"G3", "G4"}) {
		t.Fatalf("после кражи P1.Local=%v P0.Local=%v, want [G1] и [G3 G4]", s.P[1].Local, s.P[0].Local)
	}
}

func TestNoWork(t *testing.T) {
	s := newSched(3)
	if g, ok := s.FindRunnable(1); ok || g != "" {
		t.Fatalf("FindRunnable без работы = (%q, %v), want (\"\", false)", g, ok)
	}
	if s.P[1].Tick != 0 {
		t.Errorf("Tick = %d после неудачного поиска, want 0", s.P[1].Tick)
	}
}
